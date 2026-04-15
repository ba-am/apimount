package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/apimount/apimount/internal/core/auth"
	"github.com/apimount/apimount/internal/core/auth/secret"
	"github.com/apimount/apimount/internal/core/cache"
	"github.com/apimount/apimount/internal/core/exec"
	"github.com/apimount/apimount/internal/core/spec"
)

// loadedSpec carries a parsed spec together with the base URL to execute against.
type loadedSpec struct {
	ps      *spec.ParsedSpec
	baseURL string
}

// loadSpecFromFlags loads + parses the spec referenced by --spec and applies
// --base-url overrides. It errors out with a user-friendly message when --spec
// is missing or unreachable.
func loadSpecFromFlags() (*loadedSpec, error) {
	specPath := v.GetString("spec")
	if specPath == "" {
		return nil, fmt.Errorf("--spec is required (path or URL to OpenAPI spec)")
	}
	data, err := spec.LoadSpec(specPath)
	if err != nil {
		return nil, fmt.Errorf("load spec: %w", err)
	}
	ps, err := spec.Parse(data, specPath)
	if err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	baseURL := v.GetString("base-url")
	if baseURL == "" {
		baseURL = ps.BaseURL
	}
	if baseURL == "" {
		return nil, fmt.Errorf("no base URL: set --base-url or define servers[0] in the spec")
	}
	return &loadedSpec{ps: ps, baseURL: baseURL}, nil
}

// newExecutorFromFlags builds an Executor from the persistent flags on root,
// including any Phase 3 auth providers (OAuth2 client credentials today;
// SigV4 / mTLS / device-code follow-ups later).
func newExecutorFromFlags(ls *loadedSpec) (*exec.Executor, error) {
	ctx := context.Background()
	registry := secret.NewRegistry()

	bearer, err := registry.Resolve(ctx, v.GetString("auth-bearer"))
	if err != nil {
		return nil, fmt.Errorf("resolve --auth-bearer: %w", err)
	}
	apiKey, err := registry.Resolve(ctx, v.GetString("auth-apikey"))
	if err != nil {
		return nil, fmt.Errorf("resolve --auth-apikey: %w", err)
	}

	authCfg := &auth.Config{
		Bearer:      bearer,
		Basic:       v.GetString("auth-basic"),
		APIKey:      apiKey,
		APIKeyParam: v.GetString("auth-apikey-param"),
	}
	timeout := v.GetDuration("timeout")
	if timeout == 0 {
		timeout = defaultTimeout
	}

	chain, err := buildAuthChain(ctx, registry)
	if err != nil {
		return nil, err
	}

	client := exec.NewAPIClientWithChain(timeout, authCfg, ls.ps.AuthSchemes, chain)
	c := cache.New(0, 0) // cache disabled for one-shot CLI invocations
	return exec.NewExecutor(client, c, ls.baseURL, true), nil
}

// buildAuthChain assembles the Phase 3 auth.Chain from --auth-* flags. Secret
// references (env:VAR, file:path, literal:x) are resolved before handing values
// to providers, so an OAuth2 client-secret never touches argv or shell history.
func buildAuthChain(ctx context.Context, registry *secret.Registry) (*auth.Chain, error) {
	clientID := v.GetString("auth-oauth2-client-id")
	clientSecretRef := v.GetString("auth-oauth2-client-secret")
	tokenURL := v.GetString("auth-oauth2-token-url")
	scopes := v.GetStringSlice("auth-oauth2-scopes")

	// No OAuth2 flags → empty chain. The static injector still runs.
	if clientID == "" && clientSecretRef == "" && tokenURL == "" {
		return auth.NewChain(), nil
	}

	clientSecret, err := registry.Resolve(ctx, clientSecretRef)
	if err != nil {
		return nil, fmt.Errorf("resolve --auth-oauth2-client-secret: %w", err)
	}

	provider, err := auth.NewOAuth2CCProvider(auth.OAuth2CCConfig{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     tokenURL,
		Scopes:       scopes,
	})
	if err != nil {
		return nil, err
	}
	return auth.NewChain(provider), nil
}

// parseKVList parses a list of key=value strings into a map.
// Empty strings and malformed entries are skipped silently (CLI-friendly).
func parseKVList(pairs []string) map[string]string {
	out := map[string]string{}
	for _, kv := range pairs {
		if kv == "" {
			continue
		}
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		out[strings.TrimSpace(kv[:idx])] = strings.TrimSpace(kv[idx+1:])
	}
	return out
}

// readBodyFromFlag returns the request body from either --body (literal string)
// or --body-file (path). @- reads stdin.
func readBodyFromFlag(cmd *cobra.Command) ([]byte, error) {
	body, _ := cmd.Flags().GetString("body")
	bodyFile, _ := cmd.Flags().GetString("body-file")
	if body != "" && bodyFile != "" {
		return nil, fmt.Errorf("--body and --body-file are mutually exclusive")
	}
	if body != "" {
		return []byte(body), nil
	}
	if bodyFile == "" {
		return nil, nil
	}
	if bodyFile == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(bodyFile)
}

// writeJSONResponse pretty-prints a JSON body to stdout if possible, otherwise
// writes it verbatim. Always terminates with a newline.
func writeJSONResponse(w io.Writer, body []byte) {
	if len(body) == 0 {
		return
	}
	var v interface{}
	if json.Unmarshal(body, &v) == nil {
		pretty, err := json.MarshalIndent(v, "", "  ")
		if err == nil {
			_, _ = w.Write(pretty)
			_, _ = io.WriteString(w, "\n")
			return
		}
	}
	_, _ = w.Write(body)
	if len(body) > 0 && body[len(body)-1] != '\n' {
		_, _ = io.WriteString(w, "\n")
	}
}

func buildLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

