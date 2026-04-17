// Package webdav serves the API tree as a WebDAV filesystem.
// Connect from macOS Finder, Windows Explorer, or any WebDAV client.
package webdav

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/net/webdav"

	"github.com/apimount/apimount/internal/core/exec"
	"github.com/apimount/apimount/internal/core/plan"
)

// Config holds WebDAV frontend configuration.
type Config struct {
	Addr string // listen address (default ":8080")
}

// Frontend is the WebDAV frontend.
type Frontend struct {
	cfg      Config
	executor *exec.Executor
	root     *plan.FSNode
	baseURL  string
}

// New creates a new WebDAV frontend.
func New(cfg Config, root *plan.FSNode, executor *exec.Executor, baseURL string) *Frontend {
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	return &Frontend{cfg: cfg, executor: executor, root: root, baseURL: baseURL}
}

// Name implements frontend.Frontend.
func (f *Frontend) Name() string { return "webdav" }

// Serve starts the WebDAV HTTP server.
func (f *Frontend) Serve(ctx context.Context, _, _ any) error {
	handler := &webdav.Handler{
		FileSystem: &davFS{root: f.root, executor: f.executor, baseURL: f.baseURL},
		LockSystem: webdav.NewMemLS(),
	}
	fmt.Fprintf(os.Stderr, "WebDAV server listening on %s\n", f.cfg.Addr)
	srv := &http.Server{Addr: f.cfg.Addr, Handler: handler}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

type davFS struct {
	root     *plan.FSNode
	executor *exec.Executor
	baseURL  string
}

func (d *davFS) Mkdir(_ context.Context, _ string, _ os.FileMode) error {
	return os.ErrPermission
}

func (d *davFS) RemoveAll(_ context.Context, _ string) error {
	return os.ErrPermission
}

func (d *davFS) Rename(_ context.Context, _, _ string) error {
	return os.ErrPermission
}

func (d *davFS) OpenFile(_ context.Context, name string, flag int, _ os.FileMode) (webdav.File, error) {
	node := d.resolve(name)
	if node == nil {
		return nil, os.ErrNotExist
	}
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0 {
		return nil, os.ErrPermission
	}
	return &davFile{node: node, executor: d.executor, baseURL: d.baseURL, name: name}, nil
}

func (d *davFS) Stat(_ context.Context, name string) (os.FileInfo, error) {
	node := d.resolve(name)
	if node == nil {
		return nil, os.ErrNotExist
	}
	return nodeInfo(node, name), nil
}

func (d *davFS) resolve(name string) *plan.FSNode {
	name = strings.TrimPrefix(name, "/")
	if name == "" || name == "." {
		return d.root
	}
	cur := d.root
	for _, seg := range strings.Split(name, "/") {
		if seg == "" {
			continue
		}
		child, ok := cur.Children[seg]
		if !ok {
			return nil
		}
		cur = child
	}
	return cur
}

type davFile struct {
	node     *plan.FSNode
	executor *exec.Executor
	baseURL  string
	name     string
	content  []byte
	offset   int
	loaded   bool
}

func (f *davFile) Close() error { return nil }

func (f *davFile) Write(_ []byte) (int, error) {
	return 0, os.ErrPermission
}

func (f *davFile) Read(p []byte) (int, error) {
	if err := f.ensureLoaded(); err != nil {
		return 0, err
	}
	if f.offset >= len(f.content) {
		return 0, io.EOF
	}
	n := copy(p, f.content[f.offset:])
	f.offset += n
	return n, nil
}

func (f *davFile) Seek(offset int64, whence int) (int64, error) {
	if err := f.ensureLoaded(); err != nil {
		return 0, err
	}
	var newOff int64
	switch whence {
	case io.SeekStart:
		newOff = offset
	case io.SeekCurrent:
		newOff = int64(f.offset) + offset
	case io.SeekEnd:
		newOff = int64(len(f.content)) + offset
	}
	if newOff < 0 {
		return 0, os.ErrInvalid
	}
	f.offset = int(newOff)
	return newOff, nil
}

func (f *davFile) Readdir(count int) ([]fs.FileInfo, error) {
	if !f.node.IsDir() {
		return nil, os.ErrInvalid
	}
	var infos []fs.FileInfo
	for name, child := range f.node.Children {
		infos = append(infos, nodeInfo(child, name))
	}
	if count > 0 && len(infos) > count {
		infos = infos[:count]
	}
	return infos, nil
}

func (f *davFile) Stat() (fs.FileInfo, error) {
	return nodeInfo(f.node, f.name), nil
}

func (f *davFile) ensureLoaded() error {
	if f.loaded {
		return nil
	}
	f.loaded = true

	if f.node.IsDir() {
		return nil
	}

	if f.node.StaticContent != nil {
		f.content = f.node.StaticContent
		return nil
	}

	if f.node.Role == plan.FileRoleGET && f.node.Operation != nil {
		body, err := f.executor.ExecuteGET(context.Background(), f.node.Operation, f.node.PathParams, nil)
		if err != nil {
			f.content = []byte(err.Error())
			return nil
		}
		f.content = body
	}

	if f.node.Role == plan.FileRoleResponse {
		f.node.Mu.RLock()
		f.content = f.node.LastResponse
		f.node.Mu.RUnlock()
	}

	return nil
}

type nodeFileInfo struct {
	name  string
	node  *plan.FSNode
	size  int64
}

func nodeInfo(node *plan.FSNode, name string) fs.FileInfo {
	base := name
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		base = name[idx+1:]
	}
	if base == "" {
		base = "/"
	}
	var size int64
	if node.StaticContent != nil {
		size = int64(len(node.StaticContent))
	} else if !node.IsDir() {
		size = 4096
	}
	return &nodeFileInfo{name: base, node: node, size: size}
}

func (i *nodeFileInfo) Name() string      { return i.name }
func (i *nodeFileInfo) Size() int64        { return i.size }
func (i *nodeFileInfo) ModTime() time.Time { return time.Now() }
func (i *nodeFileInfo) IsDir() bool        { return i.node.IsDir() }
func (i *nodeFileInfo) Sys() interface{}   { return nil }
func (i *nodeFileInfo) Mode() fs.FileMode {
	if i.node.IsDir() {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
