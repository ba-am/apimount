package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/apimount/apimount/internal/core/auth"
	"github.com/apimount/apimount/internal/core/cache"
	"github.com/apimount/apimount/internal/core/exec"
	"github.com/apimount/apimount/internal/core/plan"
	fusefe "github.com/apimount/apimount/internal/frontend/fuse"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Expose the spec as a live server (FUSE / NFS / WebDAV / MCP)",
	Long: `Run a frontend that exposes the OpenAPI spec as a live interface.

Currently only 'fuse' is available. NFS, WebDAV, and MCP land in Phase 5.`,
}

var serveFuseCmd = &cobra.Command{
	Use:   "fuse",
	Short: "Mount the spec as a FUSE filesystem",
	RunE:  runServeFuse,
}

func init() {
	f := serveFuseCmd.Flags()
	f.String("mount", "", "mount point directory (required)")
	f.String("group-by", defaultGroupBy, "tree grouping: tags|path|flat")
	f.Duration("cache-ttl", defaultCacheTTL, "GET cache TTL (0 disables)")
	f.Int("cache-max-mb", defaultCacheMaxMB, "max cache size in MB")
	f.Bool("pretty", true, "pretty-print JSON responses")
	f.Bool("read-only", false, "disallow all write operations")
	f.Bool("allow-other", false, "FUSE allow_other option")

	serveCmd.AddCommand(serveFuseCmd)
}

func runServeFuse(cmd *cobra.Command, args []string) error {
	mountPoint, _ := cmd.Flags().GetString("mount")
	if mountPoint == "" {
		return fmt.Errorf("--mount is required")
	}
	if err := ensureMountPoint(mountPoint); err != nil {
		return err
	}

	ls, err := loadSpecFromFlags()
	if err != nil {
		return err
	}

	groupBy, _ := cmd.Flags().GetString("group-by")
	cacheTTL, _ := cmd.Flags().GetDuration("cache-ttl")
	cacheMaxMB, _ := cmd.Flags().GetInt("cache-max-mb")
	pretty, _ := cmd.Flags().GetBool("pretty")
	readOnly, _ := cmd.Flags().GetBool("read-only")
	allowOther, _ := cmd.Flags().GetBool("allow-other")
	verbose := v.GetBool("verbose")

	root := plan.BuildTree(ls.ps, groupBy)

	authCfg := &auth.Config{
		Bearer:      v.GetString("auth-bearer"),
		Basic:       v.GetString("auth-basic"),
		APIKey:      v.GetString("auth-apikey"),
		APIKeyParam: v.GetString("auth-apikey-param"),
	}
	timeout := v.GetDuration("timeout")
	if timeout == 0 {
		timeout = defaultTimeout
	}

	client := exec.NewAPIClient(timeout, authCfg, ls.ps.AuthSchemes)
	c := cache.New(cacheTTL, int64(cacheMaxMB)*1024*1024)
	c.StartEviction()
	executor := exec.NewExecutor(client, c, ls.baseURL, pretty)

	logger := buildLogger(verbose)
	cfg := &fusefe.Config{
		MountPoint: mountPoint,
		ReadOnly:   readOnly,
		AllowOther: allowOther,
		Verbose:    verbose,
	}
	fmt.Fprintf(os.Stderr, "Mounting %s at %s (Ctrl-C to unmount)\n", ls.ps.Title, mountPoint)
	return fusefe.Mount(root, client, executor, c, cfg, logger)
}

func ensureMountPoint(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("mount point %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("mount point %q is not a directory", path)
	}
	return nil
}
