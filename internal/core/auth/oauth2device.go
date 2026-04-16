package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/oauth2"
)

// OAuth2DeviceConfig configures an OAuth2 device-code flow Provider.
// This is the interactive grant for CLI users: the tool displays a URL and
// user code, the user opens the URL in a browser, enters the code, and the
// tool polls the token endpoint until authorization completes.
type OAuth2DeviceConfig struct {
	ClientID      string
	ClientSecret  string // optional; some providers (GitHub) don't require it
	TokenURL      string
	DeviceAuthURL string
	Scopes        []string
	TokenCache    *TokenCache // optional; persists tokens to disk
	CacheKey      string      // profile name used as cache key
}

// OAuth2DeviceProvider implements the device-code flow (RFC 8628).
// The flow has two phases:
//  1. Login: call Login() interactively — it returns a DevicePrompt for the
//     user to complete in their browser, then blocks polling until authorized.
//  2. Apply: on each request, reuse the cached token (refreshing if needed).
//
// If no token is cached, Apply returns an error telling the user to run
// `apimount auth login`.
type OAuth2DeviceProvider struct {
	cfg   *oauth2.Config
	cache *TokenCache
	key   string
	mu    sync.Mutex
	tok   *oauth2.Token
}

// DevicePrompt is the information displayed to the user during device-code login.
type DevicePrompt struct {
	UserCode        string
	VerificationURI string
	ExpiresInSec    int
}

// NewOAuth2DeviceProvider builds a provider from the given config.
func NewOAuth2DeviceProvider(cfg OAuth2DeviceConfig) (*OAuth2DeviceProvider, error) {
	if cfg.ClientID == "" {
		return nil, errors.New("oauth2device: client_id is required")
	}
	if cfg.TokenURL == "" {
		return nil, errors.New("oauth2device: token_url is required")
	}
	if cfg.DeviceAuthURL == "" {
		return nil, errors.New("oauth2device: device_auth_url is required")
	}

	oauthCfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL:      cfg.TokenURL,
			DeviceAuthURL: cfg.DeviceAuthURL,
		},
		Scopes: cfg.Scopes,
	}

	p := &OAuth2DeviceProvider{
		cfg:   oauthCfg,
		cache: cfg.TokenCache,
		key:   cfg.CacheKey,
	}
	if p.key == "" {
		p.key = "default"
	}
	if p.cache != nil {
		p.tok = p.cache.Get(p.key)
	}
	return p, nil
}

// Name implements Provider.
func (p *OAuth2DeviceProvider) Name() string { return "oauth2device" }

// Login performs the interactive device-code flow. It calls promptFn with the
// user code and verification URI (so the CLI can display them), then blocks
// polling the token endpoint until the user completes authorization or the
// context is cancelled.
func (p *OAuth2DeviceProvider) Login(ctx context.Context, promptFn func(DevicePrompt)) (*oauth2.Token, error) {
	resp, err := p.cfg.DeviceAuth(ctx)
	if err != nil {
		return nil, fmt.Errorf("oauth2device: device auth request: %w", err)
	}

	promptFn(DevicePrompt{
		UserCode:        resp.UserCode,
		VerificationURI: resp.VerificationURI,
	})

	tok, err := p.cfg.DeviceAccessToken(ctx, resp)
	if err != nil {
		return nil, fmt.Errorf("oauth2device: poll for token: %w", err)
	}

	p.mu.Lock()
	p.tok = tok
	p.mu.Unlock()

	if p.cache != nil {
		if err := p.cache.Put(p.key, tok); err != nil {
			return tok, fmt.Errorf("oauth2device: cache token: %w", err)
		}
	}
	return tok, nil
}

// Logout clears any cached token.
func (p *OAuth2DeviceProvider) Logout() error {
	p.mu.Lock()
	p.tok = nil
	p.mu.Unlock()

	if p.cache != nil {
		return p.cache.Delete(p.key)
	}
	return nil
}

// HasToken returns true if a valid token is available (from cache or login).
func (p *OAuth2DeviceProvider) HasToken() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tok != nil && p.tok.Valid()
}

// Apply implements Provider. It sets the Authorization header from the cached
// token. If no token is available, it returns an error directing the user to
// run `apimount auth login`.
func (p *OAuth2DeviceProvider) Apply(_ context.Context, tgt *ApplyTarget) error {
	p.mu.Lock()
	tok := p.tok
	p.mu.Unlock()

	if tok == nil {
		return errors.New("oauth2device: no token available — run `apimount auth login` first")
	}

	// Attempt refresh if token has a refresh_token and is expired/near-expiry.
	if !tok.Valid() && tok.RefreshToken != "" {
		src := p.cfg.TokenSource(context.Background(), tok)
		newTok, err := src.Token()
		if err != nil {
			return fmt.Errorf("oauth2device: refresh token: %w", err)
		}
		p.mu.Lock()
		p.tok = newTok
		p.mu.Unlock()
		if p.cache != nil {
			_ = p.cache.Put(p.key, newTok)
		}
		tok = newTok
	}

	if !tok.Valid() {
		return errors.New("oauth2device: token expired — run `apimount auth login` to re-authenticate")
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
