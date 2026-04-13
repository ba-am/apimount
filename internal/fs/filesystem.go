package fs

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"go.uber.org/zap"

	"github.com/apimount/apimount/internal/cache"
	"github.com/apimount/apimount/internal/config"
	apihttp "github.com/apimount/apimount/internal/http"
	"github.com/apimount/apimount/internal/tree"
)

// APIFS is the root FUSE filesystem node.
type APIFS struct {
	fs.Inode
	root   *tree.FSNode
	client *apihttp.APIClient
	exec   *apihttp.Executor
	cache  *cache.Cache
	cfg    *config.Config
	logger *zap.Logger
}

// Mount mounts the FUSE filesystem at the given mount point.
// It blocks until the filesystem is unmounted or a signal is received.
func Mount(
	root *tree.FSNode,
	client *apihttp.APIClient,
	exec *apihttp.Executor,
	c *cache.Cache,
	cfg *config.Config,
	logger *zap.Logger,
) error {
	apiFS := &APIFS{
		root:   root,
		client: client,
		exec:   exec,
		cache:  c,
		cfg:    cfg,
		logger: logger,
	}

	opts := &fs.Options{
		AttrTimeout:  durationPtr(time.Second),
		EntryTimeout: durationPtr(time.Second),
		MountOptions: fuse.MountOptions{
			AllowOther: cfg.AllowOther,
			FsName:     "apimount",
			Name:       "apimount",
			Debug:      cfg.Verbose,
		},
	}

	server, err := fs.Mount(cfg.MountPoint, &DirNode{
		treeNode: root,
		apifs:    apiFS,
	}, opts)
	if err != nil {
		return fmt.Errorf("could not mount FUSE filesystem: %w", err)
	}

	logger.Info("mounted",
		zap.String("mount", cfg.MountPoint),
		zap.String("spec", cfg.SpecPath),
	)

	// Handle SIGINT/SIGTERM for clean unmount
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("received signal, unmounting", zap.String("signal", sig.String()))
		if err := server.Unmount(); err != nil {
			logger.Warn("unmount error", zap.Error(err))
		}
	}()

	server.Wait()
	signal.Stop(sigCh)
	logger.Info("unmounted", zap.String("mount", cfg.MountPoint))
	return nil
}

// baseNode holds shared FUSE state — not currently used directly but
// available for embedding if shared Getattr logic is needed.
type baseNode struct {
	fs.Inode
	apifs *APIFS
}

// Getattr returns basic read-only attributes.
func (n *baseNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = 0444
	out.Size = 0
	return fs.OK
}

func durationPtr(d time.Duration) *time.Duration {
	return &d
}
