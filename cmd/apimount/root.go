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
	Short: "Universal OpenAPI adapter — CLI, MCP, WebDAV, NFS",
	Long: `apimount is a universal OpenAPI adapter. Point it at any OpenAPI 3.0/3.1
spec and it exposes every operation through whichever surface you need:

  - CLI        (primary)   apimount call/get/post/put/patch/delete
  - MCP        (planned)   apimount serve mcp  — drive APIs from Claude / agents
  - WebDAV     (planned)   apimount serve webdav
  - NFS        (planned)   apimount serve nfs

All surfaces share one execution core (auth, retry, rate-limit, pagination,
schema validation, RBAC, audit). The core has no knowledge of any frontend.`,
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
	pf.String("auth-bearer", "", "Bearer token (accepts env:VAR / file:path / literal:val)")
	pf.String("auth-basic", "", "Basic auth as user:password")
	pf.String("auth-apikey", "", "API key value (accepts env:VAR / file:path / literal:val)")
	pf.String("auth-apikey-param", "", "API key parameter name")

	// Phase 3 — OAuth2 client credentials (machine-to-machine grant).
	// Other OAuth2 flows (device code, auth code + PKCE) follow in a later drop.
	pf.String("auth-oauth2-client-id", "", "OAuth2 client ID")
	pf.String("auth-oauth2-client-secret", "", "OAuth2 client secret (accepts env:VAR / file:path / literal:val)")
	pf.String("auth-oauth2-token-url", "", "OAuth2 token endpoint URL")
	pf.StringSlice("auth-oauth2-scopes", nil, "OAuth2 scopes (comma-separated)")

	// Phase 3 — mTLS (mutual TLS client authentication).
	pf.String("auth-mtls-cert", "", "path to PEM client certificate for mTLS")
	pf.String("auth-mtls-key", "", "path to PEM client private key for mTLS")
	pf.String("auth-mtls-ca", "", "path to PEM CA bundle for server verification (optional)")

	// Phase 3 — AWS SigV4.
	pf.String("auth-sigv4-access-key", "", "AWS access key ID (accepts env:VAR / file:path)")
	pf.String("auth-sigv4-secret-key", "", "AWS secret access key (accepts env:VAR / file:path)")
	pf.String("auth-sigv4-session-token", "", "AWS session token for temporary credentials (optional)")
	pf.String("auth-sigv4-region", "", "AWS region for SigV4 signing")
	pf.String("auth-sigv4-service", "", "AWS service name for SigV4 (default: execute-api)")

	// Phase 4 — Reliability middleware flags.
	pf.Int("max-retries", 3, "maximum retry attempts for idempotent requests")
	pf.Float64("rate-limit", 10, "per-host requests per second (0 = unlimited)")
	pf.Int("rate-burst", 20, "per-host burst size for rate limiter")
	pf.Int("max-pages", 100, "maximum pages to fetch for paginated responses")
	pf.Bool("validate", false, "validate request bodies against the operation's schema before sending")

	_ = v.BindPFlags(pf)

	lf := rootCmd.Flags()
	lf.Bool("pretty", true, "pretty-print JSON responses")

	rootCmd.AddCommand(
		callCmd, getCmd, postCmd, putCmd, patchCmd, deleteCmd,
		treeCmd, validateCmd,
		profileCmd,
		authCmd,
		serveCmd,
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
	for _, key := range []string{
		"spec", "base-url",
		"auth-bearer", "auth-basic", "auth-apikey", "auth-apikey-param",
		"auth-oauth2-client-id", "auth-oauth2-client-secret",
		"auth-oauth2-token-url", "auth-oauth2-device-url", "auth-oauth2-scopes",
	} {
		if val := v.Get(profileKey + "." + key); val != nil && !v.IsSet(key) {
			v.Set(key, val)
		}
	}
}
