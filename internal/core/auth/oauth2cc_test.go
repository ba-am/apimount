package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tokenServer simulates an OAuth2 token endpoint. It expects
// grant_type=client_credentials and basic-auth'd client creds or client_id /
// client_secret in the body.
type tokenServer struct {
	issued int32
	server *httptest.Server
}

func newTokenServer(t *testing.T, clientID, clientSecret string) *tokenServer {
	t.Helper()
	ts := &tokenServer{}
	ts.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		if r.Form.Get("grant_type") != "client_credentials" {
			http.Error(w, "wrong grant_type", http.StatusBadRequest)
			return
		}
		// Accept either form-encoded client creds or HTTP basic.
		cid, csec, hasBasic := r.BasicAuth()
		if !hasBasic {
			cid = r.Form.Get("client_id")
			csec = r.Form.Get("client_secret")
		}
		if cid != clientID || csec != clientSecret {
			http.Error(w, "bad client", http.StatusUnauthorized)
			return
		}
		atomic.AddInt32(&ts.issued, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("token-%d", atomic.LoadInt32(&ts.issued)),
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	t.Cleanup(ts.server.Close)
	return ts
}

func TestOAuth2CCProvider_FetchesAndCachesToken(t *testing.T) {
	ts := newTokenServer(t, "my-id", "my-secret")

	p, err := NewOAuth2CCProvider(OAuth2CCConfig{
		ClientID:     "my-id",
		ClientSecret: "my-secret",
		TokenURL:     ts.server.URL,
		Scopes:       []string{"read:pets"},
	})
	require.NoError(t, err)

	tgt1 := &ApplyTarget{Headers: map[string]string{}}
	require.NoError(t, p.Apply(context.Background(), tgt1))
	assert.True(t, strings.HasPrefix(tgt1.Headers["Authorization"], "Bearer token-"), "got %q", tgt1.Headers["Authorization"])

	// Second call must reuse the cached token — no new issuance.
	tgt2 := &ApplyTarget{Headers: map[string]string{}}
	require.NoError(t, p.Apply(context.Background(), tgt2))
	assert.Equal(t, tgt1.Headers["Authorization"], tgt2.Headers["Authorization"])
	assert.Equal(t, int32(1), atomic.LoadInt32(&ts.issued), "token should be cached, not re-fetched")
}

func TestOAuth2CCProvider_BadCredentialsErrorsOut(t *testing.T) {
	ts := newTokenServer(t, "right-id", "right-secret")

	p, err := NewOAuth2CCProvider(OAuth2CCConfig{
		ClientID:     "right-id",
		ClientSecret: "WRONG-secret",
		TokenURL:     ts.server.URL,
	})
	require.NoError(t, err)

	err = p.Apply(context.Background(), &ApplyTarget{Headers: map[string]string{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "oauth2cc")
}

func TestOAuth2CCProvider_ValidatesConfig(t *testing.T) {
	_, err := NewOAuth2CCProvider(OAuth2CCConfig{ClientSecret: "x", TokenURL: "y"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client_id")

	_, err = NewOAuth2CCProvider(OAuth2CCConfig{ClientID: "x", TokenURL: "y"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client_secret")

	_, err = NewOAuth2CCProvider(OAuth2CCConfig{ClientID: "x", ClientSecret: "y"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token_url")
}
