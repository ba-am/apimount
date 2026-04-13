package main

import (
	"fmt"
	"os"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/apimount/apimount/internal/auth"
	"github.com/apimount/apimount/internal/cache"
	"github.com/apimount/apimount/internal/config"
	apifs "github.com/apimount/apimount/internal/fs"
	apihttp "github.com/apimount/apimount/internal/http"
	"github.com/apimount/apimount/internal/spec"
	"github.com/apimount/apimount/internal/tree"
)

var (
	version = "dev"
	cfgFile string
	v       = viper.New()
)

func main() {
	// Suppress cobra's default "usage" dump on runtime errors — only show it for flag/arg errors.
	rootCmd.SilenceUsage = true
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "apimount",
	Short: "Mount any OpenAPI spec as a FUSE filesystem",
	Long: `apimount mounts any OpenAPI 3.0/3.1 spec as a FUSE filesystem.
Interact with APIs using standard Unix tools: ls, cat, echo.`,
	RunE: runMount,
}

func init() {
	cobra.OnInitialize(initConfig)

	// Persistent flags: available to root and all subcommands
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&cfgFile, "config", "", "config file (default: ~/.apimount.yaml)")
	pf.String("spec", "", "path or URL to OpenAPI spec")
	pf.String("group-by", "tags", "tree grouping: tags|path|flat")
	pf.Bool("verbose", false, "debug logging")
	pf.String("profile", "", "use a named profile from config file")
	v.BindPFlags(pf)

	// Local flags: only for the mount command
	f := rootCmd.Flags()
	f.String("mount", "", "mount point directory (required unless --dry-run)")
	f.String("base-url", "", "override base URL from spec")
	f.String("auth-bearer", "", "Bearer token")
	f.String("auth-basic", "", "Basic auth as user:password")
	f.String("auth-apikey", "", "API key value")
	f.String("auth-apikey-param", "", "API key parameter name")
	f.Duration("timeout", 0, "HTTP request timeout (default 30s)")
	f.Duration("cache-ttl", 0, "GET cache TTL, 0 to disable (default 30s)")
	f.Int("cache-max-mb", 0, "max cache size in MB (default 50)")
	f.String("response-format", "json", "output format: json|yaml|raw")
	f.Bool("pretty", true, "pretty-print JSON responses")
	f.Bool("read-only", false, "disallow all write operations")
	f.Bool("allow-other", false, "FUSE allow_other option")
	f.Bool("dry-run", false, "print filesystem tree without mounting")
	v.BindPFlags(f)

	rootCmd.AddCommand(treeCmd, validateCmd, versionCmd, completionCmd)
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
	v.ReadInConfig()

	// Apply profile if set
	profile := v.GetString("profile")
	if profile != "" {
		profileKey := fmt.Sprintf("profiles.%s", profile)
		for _, key := range []string{"spec", "base-url", "auth-bearer", "auth-basic", "auth-apikey", "cache-ttl", "group-by", "mount"} {
			val := v.Get(profileKey + "." + key)
			if val != nil && !v.IsSet(key) {
				v.Set(key, val)
			}
		}
	}
}

func runMount(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(v)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("✗ %s", err.Error())
	}

	logger := buildLogger(cfg.Verbose)
	defer logger.Sync()

	// Load and parse spec
	fmt.Fprintf(os.Stderr, "Loading spec: %s\n", cfg.SpecPath)
	data, err := spec.LoadSpec(cfg.SpecPath)
	if err != nil {
		return fmt.Errorf("✗ %s", err.Error())
	}

	ps, err := spec.Parse(data, cfg.SpecPath)
	if err != nil {
		return fmt.Errorf("✗ %s", err.Error())
	}

	// Override base URL if provided
	if cfg.BaseURL != "" {
		ps.BaseURL = cfg.BaseURL
	}
	if ps.BaseURL == "" {
		return fmt.Errorf("✗ no base URL: set --base-url or define servers[0] in the spec")
	}

	fmt.Fprintf(os.Stderr, "Parsed %d operations from %q\n", len(ps.Operations), ps.Title)

	// Build tree
	root := tree.BuildTree(ps, cfg.GroupBy)

	if cfg.DryRun {
		fmt.Print(tree.PrintTree(root))
		return nil
	}

	// Set up HTTP client
	authCfg := &auth.Config{
		Bearer:      cfg.AuthBearer,
		Basic:       cfg.AuthBasic,
		APIKey:      cfg.AuthAPIKey,
		APIKeyParam: cfg.AuthAPIKeyParam,
	}
	client := apihttp.NewAPIClient(cfg.Timeout, authCfg, ps.AuthSchemes)
	c := cache.New(cfg.CacheTTL, int64(cfg.CacheMaxSizeMB)*1024*1024)
	c.StartEviction()
	exec := apihttp.NewExecutor(client, c, ps.BaseURL, cfg.PrettyJSON)

	// Mount
	fmt.Fprintf(os.Stderr, "Mounting at %s (Ctrl-C to unmount)\n", cfg.MountPoint)
	return apifs.Mount(root, client, exec, c, cfg, logger)
}

// treeCmd prints the filesystem tree without mounting.
var treeCmd = &cobra.Command{
	Use:   "tree",
	Short: "Print the filesystem tree for a spec (dry run)",
	RunE: func(cmd *cobra.Command, args []string) error {
		specPath, _ := cmd.Flags().GetString("spec")
		groupBy, _ := cmd.Flags().GetString("group-by")
		if specPath == "" {
			specPath = v.GetString("spec")
		}
		if specPath == "" {
			return fmt.Errorf("--spec is required")
		}

		data, err := spec.LoadSpec(specPath)
		if err != nil {
			return fmt.Errorf("✗ %s", err.Error())
		}
		ps, err := spec.Parse(data, specPath)
		if err != nil {
			return fmt.Errorf("✗ %s", err.Error())
		}

		root := tree.BuildTree(ps, groupBy)
		fmt.Print(tree.PrintTree(root))
		return nil
	},
}

// validateCmd validates a spec and prints stats.
var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate that a spec can be parsed and show stats",
	RunE: func(cmd *cobra.Command, args []string) error {
		specPath := v.GetString("spec")
		if specPath == "" && len(args) > 0 {
			specPath = args[0]
		}
		if specPath == "" {
			return fmt.Errorf("--spec or positional argument required")
		}

		data, err := spec.LoadSpec(specPath)
		if err != nil {
			return fmt.Errorf("✗ %s", err.Error())
		}
		ps, err := spec.Parse(data, specPath)
		if err != nil {
			return fmt.Errorf("✗ %s", err.Error())
		}

		methods := map[string]int{}
		for _, op := range ps.Operations {
			methods[op.Method]++
		}

		fmt.Printf("✓ Valid OpenAPI spec\n")
		fmt.Printf("  Title:      %s\n", ps.Title)
		fmt.Printf("  Version:    %s\n", ps.Version)
		fmt.Printf("  Base URL:   %s\n", ps.BaseURL)
		fmt.Printf("  Operations: %d total\n", len(ps.Operations))
		for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			if n := methods[m]; n > 0 {
				fmt.Printf("    %-8s %d\n", m, n)
			}
		}
		fmt.Printf("  Auth schemes: %d\n", len(ps.AuthSchemes))
		for _, s := range ps.AuthSchemes {
			fmt.Printf("    %s (%s)\n", s.Name, s.Type)
		}
		return nil
	},
}

// versionCmd prints the version.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("apimount %s\n", version)
	},
}

// completionCmd generates shell completion scripts.
var completionCmd = &cobra.Command{
	Use:       "completion [bash|zsh|fish|powershell]",
	Short:     "Generate shell completion scripts",
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	Args:      cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletion(os.Stdout)
		default:
			return fmt.Errorf("unsupported shell: %s", args[0])
		}
	},
}

func buildLogger(verbose bool) *zap.Logger {
	var logger *zap.Logger
	if verbose {
		logger, _ = zap.NewDevelopment()
	} else {
		cfg := zap.NewProductionConfig()
		cfg.OutputPaths = []string{"stderr"}
		logger, _ = cfg.Build()
	}
	return logger
}

