package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// OAuth2CCConfig configures an OAuth2 client-credentials Provider. This is
// the non-interactive, machine-to-machine grant: the client exchanges its
// own ID + secret for an access token, no user in the loop.
//
// Used for CI jobs, server-to-server automation, and any scenario where
// apimount is running headless. Interactive grants (device code,
// authorization code + PKCE) are Phase 3 follow-ups.
type OAuth2CCConfig struct {
	ClientID     string
	ClientSecret string
	TokenURL     string
	Scopes       []string
	// EndpointParams are extra form values some issuers require
	// (e.g. audience=https://api.example.com).
	EndpointParams map[string][]string
}

// OAuth2CCProvider fetches and caches a bearer token via the OAuth2 client
// credentials grant, refreshing automatically ~1 minute before expiry (the
// underlying oauth2.TokenSource handles that).
type OAuth2CCProvider struct {
	src  oauth2.TokenSource
	name string
	mu   sync.Mutex
}

// NewOAuth2CCProvider builds a provider from the given config. The provider
// performs no network I/O until the first Apply call.
func NewOAuth2CCProvider(cfg OAuth2CCConfig) (*OAuth2CCProvider, error) {
	if cfg.ClientID == "" {
		return nil, errors.New("oauth2cc: client_id is required")
	}
	if cfg.ClientSecret == "" {
		return nil, errors.New("oauth2cc: client_secret is required")
	}
	if cfg.TokenURL == "" {
		return nil, errors.New("oauth2cc: token_url is required")
	}
	ccCfg := &clientcredentials.Config{
		ClientID:       cfg.ClientID,
		ClientSecret:   cfg.ClientSecret,
		TokenURL:       cfg.TokenURL,
		Scopes:         cfg.Scopes,
		EndpointParams: cfg.EndpointParams,
	}
	return &OAuth2CCProvider{
		src:  oauth2.ReuseTokenSource(nil, ccCfg.TokenSource(context.Background())),
		name: "oauth2cc",
	}, nil
}

// Name implements Provider.
func (p *OAuth2CCProvider) Name() string { return p.name }

// Apply implements Provider. It fetches (or reuses a cached) access token
// and sets the Authorization header.
func (p *OAuth2CCProvider) Apply(_ context.Context, tgt *ApplyTarget) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	tok, err := p.src.Token()
	if err != nil {
		return fmt.Errorf("oauth2cc: fetch token: %w", err)
	}
	if !tok.Valid() {
		return errors.New("oauth2cc: received invalid token")
	}
	if tgt.Headers == nil {
		tgt.Headers = make(map[string]string)
	}
	tokType := tok.Type()
	if tokType == "" {
		tokType = "Bearer"
	}
	tgt.Headers["Authorization"] = tokType + " " + tok.AccessToken
	return nil
}
