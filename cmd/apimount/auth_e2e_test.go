package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCall_OAuth2ClientCredentials verifies the full Phase 3 wiring:
// the CLI reads --auth-oauth2-* flags, the secret registry resolves a
// file: reference for the client secret, the OAuth2CCProvider fetches a
// token from a test issuer, and the APIClient attaches it to the upstream
// call.
func TestCall_OAuth2ClientCredentials(t *testing.T) {
	// 1. Fake OAuth2 token issuer.
	var tokensIssued int
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "client_credentials", r.Form.Get("grant_type"))
		cid, csec, _ := r.BasicAuth()
		if cid == "" {
			cid = r.Form.Get("client_id")
			csec = r.Form.Get("client_secret")
		}
		assert.Equal(t, "myclient", cid)
		assert.Equal(t, "super-secret-value", csec)
		tokensIssued++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok-from-issuer",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenSrv.Close()

	// 2. Protected upstream API that requires the token from (1).
	var sawAuth string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		assert.Equal(t, "/pet/42", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":42,"name":"Rex"}`))
	}))
	defer apiSrv.Close()

	// 3. Client secret stored in a chmod-0600 file, referenced by "file:".
	dir := t.TempDir()
	secretPath := dir + "/client.secret"
	require.NoError(t, os.WriteFile(secretPath, []byte("super-secret-value"), 0o600))

	restore := withViper(t, map[string]interface{}{
		"spec":                       "../../testdata/petstore.yaml",
		"base-url":                   apiSrv.URL,
		"auth-oauth2-client-id":      "myclient",
		"auth-oauth2-client-secret":  "file:" + secretPath,
		"auth-oauth2-token-url":      tokenSrv.URL,
	})
	defer restore()

	out := captureStdout(t, func() {
		require.NoError(t, runHTTPCall(getCmd, "GET", "/pet/42"))
	})

	assert.Equal(t, "Bearer tok-from-issuer", sawAuth, "upstream did not receive the OAuth2 token")
	assert.Equal(t, 1, tokensIssued, "token endpoint should be hit exactly once")
	assert.Contains(t, out, "Rex")
}

