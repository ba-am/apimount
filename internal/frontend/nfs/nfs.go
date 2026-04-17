// Package nfs serves the API tree as an NFSv3 filesystem via go-nfs.
// Mount from any OS: mount -t nfs -o vers=3,nolock 127.0.0.1:/ /mnt/api
package nfs

import (
	"context"
	"fmt"
	"net"
	"os"

	gonfs "github.com/willscott/go-nfs"
	nfshelper "github.com/willscott/go-nfs/helpers"

	"github.com/apimount/apimount/internal/core/exec"
	"github.com/apimount/apimount/internal/core/plan"
)

// Config holds NFS frontend configuration.
type Config struct {
	Addr string // listen address (default ":2049")
}

// Frontend is the NFS frontend.
type Frontend struct {
	cfg      Config
	executor *exec.Executor
	root     *plan.FSNode
	baseURL  string
}

// New creates a new NFS frontend.
func New(cfg Config, root *plan.FSNode, executor *exec.Executor, baseURL string) *Frontend {
	if cfg.Addr == "" {
		cfg.Addr = ":2049"
	}
	return &Frontend{cfg: cfg, executor: executor, root: root, baseURL: baseURL}
}

// Name implements frontend.Frontend.
func (f *Frontend) Name() string { return "nfs" }

// Serve starts the NFS server.
func (f *Frontend) Serve(ctx context.Context, _, _ any) error {
	listener, err := net.Listen("tcp", f.cfg.Addr)
	if err != nil {
		return fmt.Errorf("nfs: listen %s: %w", f.cfg.Addr, err)
	}
	fmt.Fprintf(os.Stderr, "NFS server listening on %s\n", f.cfg.Addr)

	bfs := newBillyFS(f.root, f.executor, f.baseURL)
	handler := &nfsHandler{fs: bfs}
	cached := nfshelper.NewCachingHandler(handler, 1024)

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	err = gonfs.Serve(listener, cached)
	if ctx.Err() != nil {
		return nil
	}
	return err
}
