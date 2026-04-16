package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	var clientOpts []exec.ClientOption
	if certFile := v.GetString("auth-mtls-cert"); certFile != "" {
		mtlsProvider, err := auth.NewMTLSProvider(auth.MTLSConfig{
			CertFile: certFile,
			KeyFile:  v.GetString("auth-mtls-key"),
			CAFile:   v.GetString("auth-mtls-ca"),
		})
		if err != nil {
			return nil, err
		}
		clientOpts = append(clientOpts, exec.WithTLSConfig(mtlsProvider.TLSConfig()))
	}

	client := exec.NewAPIClientWithChain(timeout, authCfg, ls.ps.AuthSchemes, chain, clientOpts...)
	c := cache.New(0, 0) // cache disabled for one-shot CLI invocations
	return exec.NewExecutor(client, c, ls.baseURL, true), nil
}

// buildAuthChain assembles the Phase 3 auth.Chain from --auth-* flags. Secret
// references (env:VAR, file:path, literal:x) are resolved before handing values
// to providers, so an OAuth2 client-secret never touches argv or shell history.
func buildAuthChain(ctx context.Context, registry *secret.Registry) (*auth.Chain, error) {
	var providers []auth.Provider

	// OAuth2 provider (client-credentials or cached device-code token).
	oauth2Provider, err := buildOAuth2Provider(ctx, registry)
	if err != nil {
		return nil, err
	}
	if oauth2Provider != nil {
		providers = append(providers, oauth2Provider)
	}

	// AWS SigV4 provider.
	sigv4Provider, err := buildSigV4Provider(ctx, registry)
	if err != nil {
		return nil, err
	}
	if sigv4Provider != nil {
		providers = append(providers, sigv4Provider)
	}

	return auth.NewChain(providers...), nil
}

func buildOAuth2Provider(ctx context.Context, registry *secret.Registry) (auth.Provider, error) {
	clientID := v.GetString("auth-oauth2-client-id")
	clientSecretRef := v.GetString("auth-oauth2-client-secret")
	tokenURL := v.GetString("auth-oauth2-token-url")
	scopes := v.GetStringSlice("auth-oauth2-scopes")

	if clientID == "" && clientSecretRef == "" && tokenURL == "" {
		return tryDeviceTokenProvider()
	}

	clientSecret, err := registry.Resolve(ctx, clientSecretRef)
	if err != nil {
		return nil, fmt.Errorf("resolve --auth-oauth2-client-secret: %w", err)
	}

	if clientSecret != "" {
		return auth.NewOAuth2CCProvider(auth.OAuth2CCConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     tokenURL,
			Scopes:       scopes,
		})
	}

	return tryDeviceTokenProvider()
}

func buildSigV4Provider(ctx context.Context, registry *secret.Registry) (auth.Provider, error) {
	accessKeyRef := v.GetString("auth-sigv4-access-key")
	secretKeyRef := v.GetString("auth-sigv4-secret-key")
	region := v.GetString("auth-sigv4-region")

	if accessKeyRef == "" && secretKeyRef == "" && region == "" {
		return nil, nil
	}

	accessKey, err := registry.Resolve(ctx, accessKeyRef)
	if err != nil {
		return nil, fmt.Errorf("resolve --auth-sigv4-access-key: %w", err)
	}
	secretKey, err := registry.Resolve(ctx, secretKeyRef)
	if err != nil {
		return nil, fmt.Errorf("resolve --auth-sigv4-secret-key: %w", err)
	}
	sessionToken := v.GetString("auth-sigv4-session-token")

	return auth.NewSigV4Provider(auth.SigV4Config{
		AccessKeyID:    accessKey,
		SecretAccessKey: secretKey,
		SessionToken:   sessionToken,
		Region:         region,
		Service:        v.GetString("auth-sigv4-service"),
	})
}

// tryDeviceTokenProvider returns a device-code provider loaded from the token
// cache if one exists for the current profile, or nil if not.
func tryDeviceTokenProvider() (auth.Provider, error) {
	cache, err := auth.NewTokenCache(tokenCacheDir())
	if err != nil {
		return nil, nil
	}
	key := cacheKeyFromFlags()
	tok := cache.Get(key)
	if tok == nil {
		return nil, nil
	}
	tokenURL := v.GetString("auth-oauth2-token-url")
	clientID := v.GetString("auth-oauth2-client-id")
	if tokenURL == "" || clientID == "" {
		return nil, nil
	}
	return auth.NewOAuth2DeviceProvider(auth.OAuth2DeviceConfig{
		ClientID:      clientID,
		TokenURL:      tokenURL,
		DeviceAuthURL: tokenURL, // not used for Apply, only for Login
		Scopes:        v.GetStringSlice("auth-oauth2-scopes"),
		TokenCache:    cache,
		CacheKey:      key,
	})
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


