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
	Short: "Mount any OpenAPI spec as a filesystem or call it directly",
	Long: `apimount turns any OpenAPI 3.0/3.1 spec into a filesystem (FUSE, NFS, WebDAV),
an MCP server, or a CLI HTTP client — all sharing the same execution core.

v1 compatibility: 'apimount --spec S --mount M' still works. It prints a
deprecation warning and dispatches to 'apimount serve fuse'.`,
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
