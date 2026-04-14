// Package fuse is the FUSE frontend for apimount.
// It is one of several optional frontends; all business logic lives in internal/core/exec.
package fuse

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	gofs "github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/apimount/apimount/internal/core/cache"
	"github.com/apimount/apimount/internal/core/exec"
	"github.com/apimount/apimount/internal/core/plan"
)

// Config holds FUSE-frontend-specific configuration.
type Config struct {
	MountPoint string
	ReadOnly   bool
	AllowOther bool
	Verbose    bool
}

// APIFS is the root FUSE filesystem node.
type APIFS struct {
	gofs.Inode
	root   *plan.FSNode
	client *exec.APIClient
	ex     *exec.Executor
	cache  *cache.Cache
	cfg    *Config
	logger *slog.Logger
}

// Mount mounts the FUSE filesystem at the given mount point.
// It blocks until the filesystem is unmounted or a signal is received.
func Mount(
	root *plan.FSNode,
	client *exec.APIClient,
	ex *exec.Executor,
	c *cache.Cache,
	cfg *Config,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	apiFS := &APIFS{
		root:   root,
		client: client,
		ex:     ex,
		cache:  c,
		cfg:    cfg,
		logger: logger,
	}

	opts := &gofs.Options{
		AttrTimeout:  durationPtr(time.Second),
		EntryTimeout: durationPtr(time.Second),
		MountOptions: fuse.MountOptions{
			AllowOther: cfg.AllowOther,
			FsName:     "apimount",
			Name:       "apimount",
			Debug:      cfg.Verbose,
		},
	}

	server, err := gofs.Mount(cfg.MountPoint, &DirNode{
		treeNode: root,
		apifs:    apiFS,
	}, opts)
	if err != nil {
		return fmt.Errorf("could not mount FUSE filesystem: %w", err)
	}

	logger.Info("mounted", "mount", cfg.MountPoint)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("received signal, unmounting", "signal", sig.String())
		if err := server.Unmount(); err != nil {
			logger.Warn("unmount error", "err", err)
		}
	}()

	server.Wait()
	signal.Stop(sigCh)
	logger.Info("unmounted", "mount", cfg.MountPoint)
	return nil
}

// Serve implements frontend.Frontend.
func (a *APIFS) Serve(ctx context.Context, p any, e any) error {
	return Mount(
		p.(*plan.FSNode),
		a.client,
		e.(*exec.Executor),
		a.cache,
		a.cfg,
		a.logger,
	)
}

// Name implements frontend.Frontend.
func (a *APIFS) Name() string { return "fuse" }

type baseNode struct {
	gofs.Inode
	apifs *APIFS
}

func (n *baseNode) Getattr(_ context.Context, _ gofs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = 0444
	out.Size = 0
	return gofs.OK
}

func durationPtr(d time.Duration) *time.Duration { return &d }
