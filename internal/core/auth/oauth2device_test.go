package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuth2DeviceProvider_Validate(t *testing.T) {
	_, err := NewOAuth2DeviceProvider(OAuth2DeviceConfig{})
	assert.ErrorContains(t, err, "client_id")

	_, err = NewOAuth2DeviceProvider(OAuth2DeviceConfig{ClientID: "x"})
	assert.ErrorContains(t, err, "token_url")

	_, err = NewOAuth2DeviceProvider(OAuth2DeviceConfig{ClientID: "x", TokenURL: "http://tok"})
	assert.ErrorContains(t, err, "device_auth_url")
}

func TestOAuth2DeviceProvider_LoginAndApply(t *testing.T) {
	var polls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/device/code" {
			json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "DEVCODE",
				"user_code":        "ABCD-1234",
				"verification_uri": "https://example.com/activate",
				"expires_in":       900,
				"interval":         1,
			})
			return
		}

		if r.URL.Path == "/token" {
			n := atomic.AddInt32(&polls, 1)
			if n < 2 {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "authorization_pending",
				})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "device-tok-123",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"refresh_token": "refresh-abc",
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cache, err := NewTokenCache(t.TempDir())
	require.NoError(t, err)

	provider, err := NewOAuth2DeviceProvider(OAuth2DeviceConfig{
		ClientID:      "testclient",
		TokenURL:      srv.URL + "/token",
		DeviceAuthURL: srv.URL + "/device/code",
		Scopes:        []string{"read", "write"},
		TokenCache:    cache,
		CacheKey:      "testprofile",
	})
	require.NoError(t, err)

	var gotPrompt DevicePrompt
	tok, err := provider.Login(context.Background(), func(p DevicePrompt) {
		gotPrompt = p
	})
	require.NoError(t, err)
	assert.Equal(t, "ABCD-1234", gotPrompt.UserCode)
	assert.Equal(t, "https://example.com/activate", gotPrompt.VerificationURI)
	assert.Equal(t, "device-tok-123", tok.AccessToken)
	assert.Equal(t, "refresh-abc", tok.RefreshToken)

	tgt := &ApplyTarget{Headers: map[string]string{}}
	require.NoError(t, provider.Apply(context.Background(), tgt))
	assert.Equal(t, "Bearer device-tok-123", tgt.Headers["Authorization"])

	cached := cache.Get("testprofile")
	require.NotNil(t, cached, "token should be persisted in cache")
	assert.Equal(t, "device-tok-123", cached.AccessToken)
}

func TestOAuth2DeviceProvider_ApplyWithoutLogin(t *testing.T) {
	provider, err := NewOAuth2DeviceProvider(OAuth2DeviceConfig{
		ClientID:      "testclient",
		TokenURL:      "http://example.com/token",
		DeviceAuthURL: "http://example.com/device",
	})
	require.NoError(t, err)

	tgt := &ApplyTarget{Headers: map[string]string{}}
	err = provider.Apply(context.Background(), tgt)
	assert.ErrorContains(t, err, "no token available")
}

func TestOAuth2DeviceProvider_Logout(t *testing.T) {
	cache, err := NewTokenCache(t.TempDir())
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/device/code" {
			json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "DC",
				"user_code":        "X",
				"verification_uri": "http://x",
				"interval":         1,
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	provider, err := NewOAuth2DeviceProvider(OAuth2DeviceConfig{
		ClientID:      "c",
		TokenURL:      srv.URL + "/token",
		DeviceAuthURL: srv.URL + "/device/code",
		TokenCache:    cache,
		CacheKey:      "logout-test",
	})
	require.NoError(t, err)

	_, err = provider.Login(context.Background(), func(DevicePrompt) {})
	require.NoError(t, err)
	assert.True(t, provider.HasToken())

	require.NoError(t, provider.Logout())
	assert.False(t, provider.HasToken())
	assert.Nil(t, cache.Get("logout-test"))
}
