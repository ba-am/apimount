package nfs

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	billy "github.com/go-git/go-billy/v5"

	"github.com/apimount/apimount/internal/core/exec"
	"github.com/apimount/apimount/internal/core/plan"
)

type billyFS struct {
	root     *plan.FSNode
	executor *exec.Executor
	baseURL  string
}

func newBillyFS(root *plan.FSNode, executor *exec.Executor, baseURL string) *billyFS {
	return &billyFS{root: root, executor: executor, baseURL: baseURL}
}

func (b *billyFS) resolve(path string) *plan.FSNode {
	path = filepath.Clean(path)
	path = strings.TrimPrefix(path, "/")
	if path == "" || path == "." {
		return b.root
	}
	cur := b.root
	for _, seg := range strings.Split(path, "/") {
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

func (b *billyFS) Create(filename string) (billy.File, error) {
	return nil, os.ErrPermission
}

func (b *billyFS) Open(filename string) (billy.File, error) {
	return b.OpenFile(filename, os.O_RDONLY, 0)
}

func (b *billyFS) OpenFile(filename string, flag int, _ os.FileMode) (billy.File, error) {
	node := b.resolve(filename)
	if node == nil {
		return nil, os.ErrNotExist
	}
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0 {
		return nil, os.ErrPermission
	}
	return &billyFile{node: node, executor: b.executor, name: filepath.Base(filename)}, nil
}

func (b *billyFS) Stat(filename string) (os.FileInfo, error) {
	node := b.resolve(filename)
	if node == nil {
		return nil, os.ErrNotExist
	}
	return &billyInfo{name: filepath.Base(filename), node: node}, nil
}

func (b *billyFS) Rename(_, _ string) error { return os.ErrPermission }
func (b *billyFS) Remove(_ string) error    { return os.ErrPermission }

func (b *billyFS) Join(elem ...string) string {
	return filepath.Join(elem...)
}

func (b *billyFS) TempFile(_, _ string) (billy.File, error) {
	return nil, os.ErrPermission
}

func (b *billyFS) ReadDir(path string) ([]os.FileInfo, error) {
	node := b.resolve(path)
	if node == nil {
		return nil, os.ErrNotExist
	}
	if !node.IsDir() {
		return nil, os.ErrInvalid
	}
	var infos []os.FileInfo
	for name, child := range node.Children {
		infos = append(infos, &billyInfo{name: name, node: child})
	}
	return infos, nil
}

func (b *billyFS) MkdirAll(_ string, _ os.FileMode) error {
	return os.ErrPermission
}

func (b *billyFS) Lstat(filename string) (os.FileInfo, error) {
	return b.Stat(filename)
}

func (b *billyFS) Symlink(_, _ string) error        { return os.ErrPermission }
func (b *billyFS) Readlink(_ string) (string, error) { return "", os.ErrPermission }

func (b *billyFS) Chroot(path string) (billy.Filesystem, error) {
	node := b.resolve(path)
	if node == nil {
		return nil, os.ErrNotExist
	}
	return &billyFS{root: node, executor: b.executor, baseURL: b.baseURL}, nil
}

func (b *billyFS) Root() string { return "/" }

type billyFile struct {
	node     *plan.FSNode
	executor *exec.Executor
	name     string
	reader   *bytes.Reader
	loaded   bool
}

func (f *billyFile) Name() string { return f.name }

func (f *billyFile) Read(p []byte) (int, error) {
	if err := f.ensureLoaded(); err != nil {
		return 0, err
	}
	return f.reader.Read(p)
}

func (f *billyFile) ReadAt(p []byte, off int64) (int, error) {
	if err := f.ensureLoaded(); err != nil {
		return 0, err
	}
	return f.reader.ReadAt(p, off)
}

func (f *billyFile) Seek(offset int64, whence int) (int64, error) {
	if err := f.ensureLoaded(); err != nil {
		return 0, err
	}
	return f.reader.Seek(offset, whence)
}

func (f *billyFile) Write(_ []byte) (int, error)  { return 0, os.ErrPermission }
func (f *billyFile) Close() error                  { return nil }
func (f *billyFile) Lock() error                   { return nil }
func (f *billyFile) Unlock() error                 { return nil }
func (f *billyFile) Truncate(_ int64) error        { return os.ErrPermission }

func (f *billyFile) ensureLoaded() error {
	if f.loaded {
		return nil
	}
	f.loaded = true

	var content []byte
	if f.node.StaticContent != nil {
		content = f.node.StaticContent
	} else if f.node.Role == plan.FileRoleGET && f.node.Operation != nil {
		body, err := f.executor.ExecuteGET(context.Background(), f.node.Operation, f.node.PathParams, nil)
		if err != nil {
			content = []byte(err.Error())
		} else {
			content = body
		}
	} else if f.node.Role == plan.FileRoleResponse {
		f.node.Mu.RLock()
		content = f.node.LastResponse
		f.node.Mu.RUnlock()
	}
	if content == nil {
		content = []byte{}
	}
	f.reader = bytes.NewReader(content)
	return nil
}

type billyInfo struct {
	name string
	node *plan.FSNode
}

func (i *billyInfo) Name() string { return i.name }
func (i *billyInfo) Size() int64 {
	if i.node.StaticContent != nil {
		return int64(len(i.node.StaticContent))
	}
	if i.node.IsDir() {
		return 0
	}
	return 4096
}
func (i *billyInfo) Mode() fs.FileMode {
	if i.node.IsDir() {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i *billyInfo) ModTime() time.Time  { return time.Now() }
func (i *billyInfo) IsDir() bool         { return i.node.IsDir() }
func (i *billyInfo) Sys() interface{}    { return nil }

// Verify interface compliance at compile time.
var _ billy.Filesystem = (*billyFS)(nil)
var _ billy.File = (*billyFile)(nil)
var _ io.ReaderAt = (*billyFile)(nil)
