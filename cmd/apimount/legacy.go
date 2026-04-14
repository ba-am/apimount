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

// runLegacyRoot implements the v1 form `apimount --spec S --mount M` for
// backwards compatibility. It prints a deprecation warning pointing at
// `apimount serve fuse`, then executes the same mount flow.
//
// When run with no args at all, it prints the root command's help.
func runLegacyRoot(cmd *cobra.Command, args []string) error {
	mountPoint, _ := cmd.Flags().GetString("mount")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	specPath := v.GetString("spec")

	// No spec and no mount → show help.
	if specPath == "" && mountPoint == "" && !dryRun {
		return cmd.Help()
	}

	// Deprecation notice — always print to stderr so scripts capturing stdout
	// stay clean.
	fmt.Fprintln(os.Stderr, "warning: 'apimount --spec ... --mount ...' is a v1 form; prefer 'apimount serve fuse --spec ... --mount ...'")

	ls, err := loadSpecFromFlags()
	if err != nil {
		return err
	}

	groupBy, _ := cmd.Flags().GetString("group-by")
	if groupBy == "" {
		groupBy = defaultGroupBy
	}
	root := plan.BuildTree(ls.ps, groupBy)

	if dryRun {
		fmt.Print(plan.PrintTree(root))
		return nil
	}
	if mountPoint == "" {
		return fmt.Errorf("--mount is required (or use --dry-run, or 'apimount tree')")
	}
	if err := ensureMountPoint(mountPoint); err != nil {
		return err
	}

	cacheTTL, _ := cmd.Flags().GetDuration("cache-ttl")
	if cacheTTL == 0 {
		cacheTTL = defaultCacheTTL
	}
	cacheMaxMB, _ := cmd.Flags().GetInt("cache-max-mb")
	if cacheMaxMB == 0 {
		cacheMaxMB = defaultCacheMaxMB
	}
	pretty, _ := cmd.Flags().GetBool("pretty")
	readOnly, _ := cmd.Flags().GetBool("read-only")
	allowOther, _ := cmd.Flags().GetBool("allow-other")
	verbose := v.GetBool("verbose")

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
