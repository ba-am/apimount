package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withViper replaces the package-global v with a fresh isolated Viper,
// returning a restore func. Use in tests that need to set CLI flags without
// colliding with cobra's persistent-flag bindings on rootCmd.
func withViper(t *testing.T, settings map[string]interface{}) func() {
	t.Helper()
	prev := v
	fresh := viper.New()
	for k, val := range settings {
		fresh.Set(k, val)
	}
	v = fresh
	return func() { v = prev }
}

// captureStdout redirects os.Stdout to a buffer for the duration of fn.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	prev := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()
	_ = w.Close()
	<-done
	os.Stdout = prev
	return buf.String()
}

func TestCall_GET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/pet/42", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":42,"name":"Rex"}`))
	}))
	defer srv.Close()

	restore := withViper(t, map[string]interface{}{
		"spec":     "../../testdata/petstore.yaml",
		"base-url": srv.URL,
	})
	defer restore()

	out := captureStdout(t, func() {
		require.NoError(t, runHTTPCall(getCmd, "GET", "/pet/42"))
	})
	assert.Contains(t, out, "Rex")
	assert.Contains(t, out, "42")
}

func TestCall_POST_WithBodyFlag(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/pet", r.URL.Path)
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":99,"name":"NewPet"}`))
	}))
	defer srv.Close()

	restore := withViper(t, map[string]interface{}{
		"spec":     "../../testdata/petstore.yaml",
		"base-url": srv.URL,
	})
	defer restore()

	require.NoError(t, postCmd.Flags().Set("body", `{"name":"NewPet","photoUrls":[]}`))
	defer func() { _ = postCmd.Flags().Set("body", "") }()

	out := captureStdout(t, func() {
		require.NoError(t, runHTTPCall(postCmd, "POST", "/pet"))
	})
	assert.Equal(t, `{"name":"NewPet","photoUrls":[]}`, string(gotBody))
	assert.Contains(t, out, "99")
}

func TestCall_NoMatchingOperation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	restore := withViper(t, map[string]interface{}{
		"spec":     "../../testdata/petstore.yaml",
		"base-url": srv.URL,
	})
	defer restore()

	err := runHTTPCall(getCmd, "GET", "/does-not-exist")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "no operation"))
}
