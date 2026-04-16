package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestTokenCache_PutGet(t *testing.T) {
	tc, err := NewTokenCache(t.TempDir())
	require.NoError(t, err)

	tok := &oauth2.Token{
		AccessToken:  "abc123",
		TokenType:    "Bearer",
		RefreshToken: "refresh-xyz",
		Expiry:       time.Now().Add(1 * time.Hour),
	}
	require.NoError(t, tc.Put("myprofile", tok))

	got := tc.Get("myprofile")
	require.NotNil(t, got)
	assert.Equal(t, "abc123", got.AccessToken)
	assert.Equal(t, "Bearer", got.TokenType)
	assert.Equal(t, "refresh-xyz", got.RefreshToken)
}

func TestTokenCache_ExpiredWithoutRefresh(t *testing.T) {
	tc, err := NewTokenCache(t.TempDir())
	require.NoError(t, err)

	tok := &oauth2.Token{
		AccessToken: "expired",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(-1 * time.Minute),
	}
	require.NoError(t, tc.Put("expired", tok))
	assert.Nil(t, tc.Get("expired"))
}

func TestTokenCache_ExpiredWithRefreshToken(t *testing.T) {
	tc, err := NewTokenCache(t.TempDir())
	require.NoError(t, err)

	tok := &oauth2.Token{
		AccessToken:  "expired",
		TokenType:    "Bearer",
		RefreshToken: "can-refresh",
		Expiry:       time.Now().Add(-1 * time.Minute),
	}
	require.NoError(t, tc.Put("refreshable", tok))
	got := tc.Get("refreshable")
	require.NotNil(t, got, "should return token with refresh_token even if expired")
	assert.Equal(t, "can-refresh", got.RefreshToken)
}

func TestTokenCache_Delete(t *testing.T) {
	tc, err := NewTokenCache(t.TempDir())
	require.NoError(t, err)

	tok := &oauth2.Token{AccessToken: "del", TokenType: "Bearer", Expiry: time.Now().Add(1 * time.Hour)}
	require.NoError(t, tc.Put("todelete", tok))
	assert.NotNil(t, tc.Get("todelete"))

	require.NoError(t, tc.Delete("todelete"))
	assert.Nil(t, tc.Get("todelete"))
}

func TestTokenCache_DeleteNonExistent(t *testing.T) {
	tc, err := NewTokenCache(t.TempDir())
	require.NoError(t, err)
	assert.NoError(t, tc.Delete("nope"))
}

func TestTokenCache_MissingKey(t *testing.T) {
	tc, err := NewTokenCache(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, tc.Get("nonexistent"))
}
