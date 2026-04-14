package fuse

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"syscall"

	gofs "github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/apimount/apimount/internal/core/plan"
)

// FileNode is a FUSE inode representing a virtual file.
type FileNode struct {
	gofs.Inode
	treeNode *plan.FSNode
	apifs    *APIFS
}

var _ gofs.NodeGetattrer = (*FileNode)(nil)
var _ gofs.NodeOpener = (*FileNode)(nil)
var _ gofs.NodeSetattrer = (*FileNode)(nil)

func (f *FileNode) Getattr(_ context.Context, _ gofs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = fileMode(f.treeNode)
	if f.treeNode.StaticContent != nil {
		out.Size = uint64(len(f.treeNode.StaticContent))
	} else {
		f.treeNode.Mu.RLock()
		sz := uint64(len(f.treeNode.LastResponse))
		f.treeNode.Mu.RUnlock()
		if sz == 0 {
			sz = 4096
		}
		out.Size = sz
	}
	return gofs.OK
}

// Setattr accepts size/mode changes (needed for O_TRUNC on Linux write redirection).
func (f *FileNode) Setattr(_ context.Context, _ gofs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	out.Mode = fileMode(f.treeNode)
	out.Size = in.Size
	return gofs.OK
}

func (f *FileNode) Open(_ context.Context, flags uint32) (gofs.FileHandle, uint32, syscall.Errno) {
	fh := &fileHandle{
		node:  f,
		flags: flags,
		buf:   &bytes.Buffer{},
	}
	return fh, fuse.FOPEN_DIRECT_IO, gofs.OK
}

// fileHandle holds per-open-file state.
type fileHandle struct {
	node        *FileNode
	flags       uint32
	buf         *bytes.Buffer
	content     []byte
	contentOnce bool
	flushed     bool
	mu          sync.Mutex
}

var _ gofs.FileReader = (*fileHandle)(nil)
var _ gofs.FileWriter = (*fileHandle)(nil)
var _ gofs.FileReleaser = (*fileHandle)(nil)
var _ gofs.FileFlusher = (*fileHandle)(nil)

// Read serves file content. For GET files, executes HTTP on first read.
func (fh *fileHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	if !fh.contentOnce {
		content, err := fh.fetchContent(ctx)
		if err != nil {
			fh.node.apifs.logger.Debug("read error", "err", err)
			content = []byte(err.Error() + "\n")
		}
		fh.content = content
		fh.contentOnce = true
	}

	if off >= int64(len(fh.content)) {
		return fuse.ReadResultData(nil), gofs.OK
	}
	end := off + int64(len(dest))
	if end > int64(len(fh.content)) {
		end = int64(len(fh.content))
	}
	return fuse.ReadResultData(fh.content[off:end]), gofs.OK
}

func (fh *fileHandle) fetchContent(ctx context.Context) ([]byte, error) {
	n := fh.node.treeNode
	apifs := fh.node.apifs

	switch n.Role {
	case plan.FileRoleHelp:
		return resolveHelpContent(n), nil

	case plan.FileRoleSchema:
		if n.StaticContent != nil {
			return n.StaticContent, nil
		}
		return []byte("{}\n"), nil

	case plan.FileRoleResponse:
		n.Mu.RLock()
		resp := make([]byte, len(n.LastResponse))
		copy(resp, n.LastResponse)
		n.Mu.RUnlock()
		if len(resp) == 0 {
			return []byte("(no response yet — read .data or perform a write operation)\n"), nil
		}
		return resp, nil

	case plan.FileRoleGET:
		params := copyParams(n.QueryParams)
		if n.Parent != nil {
			if qNode, ok := n.Parent.Children[".query"]; ok {
				qNode.Mu.RLock()
				for k, v := range qNode.QueryParams {
					params[k] = v
				}
				qNode.Mu.RUnlock()
			}
		}
		body, _, err := apifs.ex.ExecuteGET(ctx, n.Operation, n.PathParams, params)
		storeResponse(n, body)
		if err != nil {
			return body, nil
		}
		return body, nil

	case plan.FileRoleQuery:
		n.Mu.RLock()
		params := copyParams(n.QueryParams)
		n.Mu.RUnlock()
		if len(params) == 0 {
			return []byte("(write query params as key=val&key2=val2, then read to execute GET)\n"), nil
		}
		body, _, _ := apifs.ex.ExecuteGET(ctx, n.Operation, n.PathParams, params)
		storeResponse(n, body)
		return body, nil

	case plan.FileRolePost, plan.FileRolePut, plan.FileRolePatch, plan.FileRoleDelete:
		n.Mu.RLock()
		resp := make([]byte, len(n.LastResponse))
		copy(resp, n.LastResponse)
		n.Mu.RUnlock()
		if len(resp) == 0 {
			return []byte(roleHint(n.Role)), nil
		}
		return resp, nil
	}

	return []byte{}, nil
}

func resolveHelpContent(n *plan.FSNode) []byte {
	if n.StaticContent == nil {
		return []byte("(no help available)\n")
	}
	content := string(n.StaticContent)
	for param, value := range n.PathParams {
		content = strings.ReplaceAll(content, "{"+param+"}", value)
	}
	return []byte(content)
}

func roleHint(role plan.FileRole) string {
	switch role {
	case plan.FileRolePost:
		return "(write JSON body to execute POST, then read for response)\n"
	case plan.FileRolePut:
		return "(write JSON body to execute PUT, then read for response)\n"
	case plan.FileRolePatch:
		return "(write JSON body to execute PATCH, then read for response)\n"
	case plan.FileRoleDelete:
		return "(write anything to execute DELETE, then read for response)\n"
	}
	return ""
}

// Write buffers data; execution happens on Flush/Release.
func (fh *fileHandle) Write(_ context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	n := fh.node.treeNode
	if fh.node.apifs.cfg.ReadOnly {
		return 0, syscall.EPERM
	}
	switch n.Role {
	case plan.FileRolePost, plan.FileRolePut, plan.FileRolePatch, plan.FileRoleDelete, plan.FileRoleQuery:
		if off == 0 {
			fh.buf.Reset()
		}
		fh.buf.Write(data)
		return uint32(len(data)), gofs.OK
	default:
		return 0, syscall.EPERM
	}
}

// Flush is called on each close(). Execute here for macOS (FUSE_FLUSH always complete).
func (fh *fileHandle) Flush(ctx context.Context) syscall.Errno {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	if fh.flushed {
		return gofs.OK
	}
	if fh.buf.Len() > 0 {
		errno := fh.executeWrite(ctx)
		fh.flushed = true
		return errno
	}
	return gofs.OK
}

// Release is the final cleanup. On Linux, FUSE_WRITE may arrive after FUSE_FLUSH.
func (fh *fileHandle) Release(_ context.Context) syscall.Errno {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	if !fh.flushed && fh.buf.Len() > 0 {
		_ = fh.executeWrite(context.Background())
		fh.flushed = true
	}
	return gofs.OK
}

func (fh *fileHandle) executeWrite(ctx context.Context) syscall.Errno {
	n := fh.node.treeNode
	apifs := fh.node.apifs
	data := fh.buf.Bytes()

	switch n.Role {
	case plan.FileRoleQuery:
		if len(data) == 0 {
			return gofs.OK
		}
		parsed := parseQueryString(strings.TrimSpace(string(data)))
		n.Mu.Lock()
		n.QueryParams = parsed
		n.Mu.Unlock()
		return gofs.OK

	case plan.FileRolePost, plan.FileRolePut, plan.FileRolePatch:
		if len(data) == 0 {
			return gofs.OK
		}
		body, errno, err := apifs.ex.ExecuteWrite(ctx, n.Operation, n.PathParams, nil, data)
		if err != nil {
			apifs.logger.Debug("write error",
				"method", n.Operation.Method,
				"path", n.Operation.Path,
				"err", err,
			)
		}
		if len(body) == 0 {
			body = []byte("OK\n")
		}
		storeResponse(n, body)
		return errno

	case plan.FileRoleDelete:
		body, errno, err := apifs.ex.ExecuteWrite(ctx, n.Operation, n.PathParams, nil, nil)
		if err != nil {
			apifs.logger.Debug("delete error", "path", n.Operation.Path, "err", err)
		}
		storeResponse(n, body)
		return errno
	}

	return gofs.OK
}

// storeResponse writes the response into the operation file and the sibling .response file.
func storeResponse(n *plan.FSNode, body []byte) {
	formatted := prettyFormat(body)

	n.Mu.Lock()
	n.LastResponse = formatted
	n.Mu.Unlock()

	if n.Parent != nil {
		if respFile, ok := n.Parent.Children[".response"]; ok && respFile != n {
			respFile.Mu.Lock()
			respFile.LastResponse = formatted
			respFile.Mu.Unlock()
		}
	}
}

func prettyFormat(body []byte) []byte {
	if json.Valid(body) {
		if pretty, err := json.MarshalIndent(json.RawMessage(body), "", "  "); err == nil {
			return append(pretty, '\n')
		}
	}
	return body
}

func parseQueryString(s string) map[string]string {
	result := make(map[string]string)
	for _, part := range strings.Split(s, "&") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.IndexByte(part, '='); idx < 0 {
			result[part] = ""
		} else {
			result[strings.TrimSpace(part[:idx])] = strings.TrimSpace(part[idx+1:])
		}
	}
	return result
}

func copyParams(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
