package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	v       = viper.New()
)

var rootCmd = &cobra.Command{
	Use:   "apimount",
	Short: "Universal OpenAPI adapter — CLI, MCP, WebDAV, NFS, FUSE",
	Long: `apimount is a universal OpenAPI adapter. Point it at any OpenAPI 3.0/3.1
spec and it exposes every operation through whichever surface you need:

  - CLI        (primary)   apimount call/get/post/put/patch/delete
  - MCP        (planned)   apimount serve mcp  — drive APIs from Claude / agents
  - WebDAV     (planned)   apimount serve webdav
  - NFS        (planned)   apimount serve nfs
  - FUSE       (optional)  apimount serve fuse — local, needs macFUSE/libfuse3

All surfaces share one execution core (auth, retry, rate-limit, pagination,
schema validation, RBAC, audit). The core has no knowledge of any frontend.

v1 compatibility: 'apimount --spec S --mount M' still works; it prints a
deprecation notice and dispatches to 'apimount serve fuse'.`,
	RunE: runLegacyRoot,
}

func init() {
	cobra.OnInitialize(initConfig)

	pf := rootCmd.PersistentFlags()
	pf.StringVar(&cfgFile, "config", "", "config file (default: ~/.apimount.yaml)")
	pf.String("spec", "", "path or URL to OpenAPI spec")
	pf.String("base-url", "", "override base URL from spec")
	pf.String("profile", "", "use a named profile from config file")
	pf.Bool("verbose", false, "debug logging")
	pf.Duration("timeout", 0, "HTTP request timeout (default 30s)")
	pf.String("auth-bearer", "", "Bearer token")
	pf.String("auth-basic", "", "Basic auth as user:password")
	pf.String("auth-apikey", "", "API key value")
	pf.String("auth-apikey-param", "", "API key parameter name")
	_ = v.BindPFlags(pf)

	// v1 legacy flags — valid only on the bare 'apimount' form.
	// The 'apimount serve fuse' subcommand defines its own copies.
	lf := rootCmd.Flags()
	lf.String("mount", "", "v1 legacy: mount point (use 'apimount serve fuse --mount')")
	lf.String("group-by", "tags", "v1 legacy: tree grouping")
	lf.Duration("cache-ttl", 0, "v1 legacy: GET cache TTL")
	lf.Int("cache-max-mb", 0, "v1 legacy: max cache MB")
	lf.Bool("pretty", true, "pretty-print JSON responses")
	lf.Bool("read-only", false, "v1 legacy: disallow writes")
	lf.Bool("allow-other", false, "v1 legacy: FUSE allow_other")
	lf.Bool("dry-run", false, "v1 legacy: print tree (use 'apimount tree')")

	rootCmd.AddCommand(
		serveCmd,
		callCmd, getCmd, postCmd, putCmd, patchCmd, deleteCmd,
		treeCmd, validateCmd,
		profileCmd,
		doctorCmd,
		specCmd,
		versionCmd, completionCmd,
	)
}

func initConfig() {
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		home, _ := os.UserHomeDir()
		v.AddConfigPath(home)
		v.SetConfigName(".apimount")
		v.SetConfigType("yaml")
	}
	v.SetEnvPrefix("APIMOUNT")
	v.AutomaticEnv()
	_ = v.ReadInConfig()

	profile := v.GetString("profile")
	if profile == "" {
		return
	}
	profileKey := fmt.Sprintf("profiles.%s", profile)
	for _, key := range []string{"spec", "base-url", "auth-bearer", "auth-basic", "auth-apikey", "cache-ttl", "group-by", "mount"} {
		if val := v.Get(profileKey + "." + key); val != nil && !v.IsSet(key) {
			v.Set(key, val)
		}
	}
}
