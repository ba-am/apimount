package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// TokenCache persists OAuth2 tokens on disk so they survive process restarts.
// Tokens are stored per-profile in a directory like ~/.apimount/tokens/.
// Files are chmod 0600.
type TokenCache struct {
	dir string
	mu  sync.Mutex
}

type cachedToken struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
}

// NewTokenCache creates a token cache rooted at the given directory.
// The directory is created if it doesn't exist.
func NewTokenCache(dir string) (*TokenCache, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("token cache: create dir: %w", err)
	}
	return &TokenCache{dir: dir}, nil
}

// Get retrieves a cached token for the given key. Returns nil if not found or expired.
func (tc *TokenCache) Get(key string) *oauth2.Token {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	data, err := os.ReadFile(tc.path(key))
	if err != nil {
		return nil
	}
	var ct cachedToken
	if err := json.Unmarshal(data, &ct); err != nil {
		return nil
	}
	tok := &oauth2.Token{
		AccessToken:  ct.AccessToken,
		TokenType:    ct.TokenType,
		RefreshToken: ct.RefreshToken,
		Expiry:       ct.Expiry,
	}
	if !ct.Expiry.IsZero() && time.Until(ct.Expiry) < 30*time.Second {
		if ct.RefreshToken != "" {
			return tok
		}
		return nil
	}
	return tok
}

// Put stores a token for the given key.
func (tc *TokenCache) Put(key string, tok *oauth2.Token) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	ct := cachedToken{
		AccessToken:  tok.AccessToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
		Expiry:       tok.Expiry,
	}
	data, err := json.MarshalIndent(ct, "", "  ")
	if err != nil {
		return fmt.Errorf("token cache: marshal: %w", err)
	}
	return os.WriteFile(tc.path(key), data, 0o600)
}

// Delete removes a cached token for the given key.
func (tc *TokenCache) Delete(key string) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	err := os.Remove(tc.path(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (tc *TokenCache) path(key string) string {
	safe := filepath.Base(key)
	return filepath.Join(tc.dir, safe+".json")
}
