package fs

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"go.uber.org/zap"

	"github.com/apimount/apimount/internal/tree"
)

// FileNode is a FUSE inode representing a virtual file.
type FileNode struct {
	fs.Inode
	treeNode *tree.FSNode
	apifs    *APIFS
}

var _ fs.NodeGetattrer = (*FileNode)(nil)
var _ fs.NodeOpener = (*FileNode)(nil)

// Getattr returns file attributes.
func (f *FileNode) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
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
	return fs.OK
}

// Open opens the file and returns a FileHandle.
func (f *FileNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	fh := &fileHandle{
		node:  f,
		flags: flags,
		buf:   &bytes.Buffer{},
	}
	return fh, fuse.FOPEN_DIRECT_IO, fs.OK
}

// fileHandle holds per-open-file state.
type fileHandle struct {
	node        *FileNode
	flags       uint32
	buf         *bytes.Buffer
	content     []byte
	contentOnce bool
	flushed     bool      // guard against double-Flush
	mu          sync.Mutex
}

var _ fs.FileReader = (*fileHandle)(nil)
var _ fs.FileWriter = (*fileHandle)(nil)
var _ fs.FileReleaser = (*fileHandle)(nil)
var _ fs.FileFlusher = (*fileHandle)(nil)

// Read serves file content. For GET files, executes HTTP on first read.
func (fh *fileHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	if !fh.contentOnce {
		content, err := fh.fetchContent(ctx)
		if err != nil {
			fh.node.apifs.logger.Debug("read error", zap.Error(err))
			content = []byte(err.Error() + "\n")
		}
		fh.content = content
		fh.contentOnce = true
	}

	if off >= int64(len(fh.content)) {
		return fuse.ReadResultData(nil), fs.OK
	}
	end := off + int64(len(dest))
	if end > int64(len(fh.content)) {
		end = int64(len(fh.content))
	}
	return fuse.ReadResultData(fh.content[off:end]), fs.OK
}

func (fh *fileHandle) fetchContent(ctx context.Context) ([]byte, error) {
	n := fh.node.treeNode
	apifs := fh.node.apifs

	switch n.Role {
	case tree.FileRoleHelp:
		return resolveHelpContent(n), nil

	case tree.FileRoleSchema:
		if n.StaticContent != nil {
			return n.StaticContent, nil
		}
		return []byte("{}\n"), nil

	case tree.FileRoleResponse:
		n.Mu.RLock()
		resp := make([]byte, len(n.LastResponse))
		copy(resp, n.LastResponse)
		n.Mu.RUnlock()
		if len(resp) == 0 {
			return []byte("(no response yet — read .data or perform a write operation)\n"), nil
		}
		return resp, nil

	case tree.FileRoleGET:
		body, _, err := apifs.exec.ExecuteGET(ctx, n.Operation, n.PathParams, n.QueryParams)
		storeResponse(n, body)
		if err != nil {
			return body, nil // return error body, not an error (so cat shows the message)
		}
		return body, nil

	case tree.FileRoleQuery:
		n.Mu.RLock()
		params := copyParams(n.QueryParams)
		n.Mu.RUnlock()
		if len(params) == 0 {
			return []byte("(write query params as key=val&key2=val2, then read to execute GET)\n"), nil
		}
		body, _, _ := apifs.exec.ExecuteGET(ctx, n.Operation, n.PathParams, params)
		storeResponse(n, body)
		return body, nil

	case tree.FileRolePost, tree.FileRolePut, tree.FileRolePatch, tree.FileRoleDelete:
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

// resolveHelpContent returns help text with path params substituted into the path.
func resolveHelpContent(n *tree.FSNode) []byte {
	if n.StaticContent == nil {
		return []byte("(no help available)\n")
	}
	content := string(n.StaticContent)
	// Replace any {paramName} occurrences in the displayed paths with the bound values.
	for param, value := range n.PathParams {
		content = strings.ReplaceAll(content, "{"+param+"}", value)
	}
	return []byte(content)
}

func roleHint(role tree.FileRole) string {
	switch role {
	case tree.FileRolePost:
		return "(write JSON body to execute POST, then read for response)\n"
	case tree.FileRolePut:
		return "(write JSON body to execute PUT, then read for response)\n"
	case tree.FileRolePatch:
		return "(write JSON body to execute PATCH, then read for response)\n"
	case tree.FileRoleDelete:
		return "(write anything to execute DELETE, then read for response)\n"
	}
	return ""
}

// Write buffers data; execution happens on Flush.
func (fh *fileHandle) Write(ctx context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	n := fh.node.treeNode
	if fh.node.apifs.cfg.ReadOnly {
		return 0, syscall.EPERM
	}

	switch n.Role {
	case tree.FileRolePost, tree.FileRolePut, tree.FileRolePatch, tree.FileRoleDelete, tree.FileRoleQuery:
		if off == 0 {
			fh.buf.Reset()
		}
		fh.buf.Write(data)
		return uint32(len(data)), fs.OK
	default:
		return 0, syscall.EPERM
	}
}

// Flush executes the buffered write operation. Called once per close.
func (fh *fileHandle) Flush(ctx context.Context) syscall.Errno {
	fh.mu.Lock()
	defer fh.mu.Unlock()
	if fh.flushed {
		return fs.OK // guard against duplicate flushes from dup'd fds
	}
	errno := fh.executeWrite(ctx)
	fh.flushed = true
	return errno
}

// Release is the final cleanup after all file descriptors are closed.
func (fh *fileHandle) Release(ctx context.Context) syscall.Errno {
	return fs.OK
}

func (fh *fileHandle) executeWrite(ctx context.Context) syscall.Errno {
	n := fh.node.treeNode
	apifs := fh.node.apifs
	data := fh.buf.Bytes()

	switch n.Role {
	case tree.FileRoleQuery:
		if len(data) == 0 {
			return fs.OK
		}
		parsed := parseQueryString(strings.TrimSpace(string(data)))
		n.Mu.Lock()
		n.QueryParams = parsed
		n.Mu.Unlock()
		return fs.OK

	case tree.FileRolePost, tree.FileRolePut, tree.FileRolePatch:
		if len(data) == 0 {
			return fs.OK
		}
		body, errno, err := apifs.exec.ExecuteWrite(ctx, n.Operation, n.PathParams, nil, data)
		if err != nil {
			apifs.logger.Debug("write error",
				zap.String("method", n.Operation.Method),
				zap.String("path", n.Operation.Path),
				zap.Error(err),
			)
		}
		storeResponse(n, body)
		return errno

	case tree.FileRoleDelete:
		// DELETE executes on any write (including empty — e.g. echo "" > .delete)
		body, errno, err := apifs.exec.ExecuteWrite(ctx, n.Operation, n.PathParams, nil, nil)
		if err != nil {
			apifs.logger.Debug("delete error",
				zap.String("path", n.Operation.Path),
				zap.Error(err),
			)
		}
		storeResponse(n, body)
		return errno
	}

	return fs.OK
}

// storeResponse writes the response into the operation file and the sibling .response file.
func storeResponse(n *tree.FSNode, body []byte) {
	formatted := prettyFormat(body)

	n.Mu.Lock()
	n.LastResponse = formatted
	n.Mu.Unlock()

	// Propagate to the sibling .response file (different mutex — safe)
	if n.Parent != nil {
		if respFile, ok := n.Parent.Children[".response"]; ok && respFile != n {
			respFile.Mu.Lock()
			respFile.LastResponse = formatted
			respFile.Mu.Unlock()
		}
	}
}

// prettyFormat pretty-prints JSON if valid, otherwise returns as-is.
func prettyFormat(body []byte) []byte {
	if json.Valid(body) {
		if pretty, err := json.MarshalIndent(json.RawMessage(body), "", "  "); err == nil {
			return append(pretty, '\n')
		}
	}
	return body
}

// parseQueryString parses "key=val&key2=val2" into a map.
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
