package exec_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimount/apimount/internal/core/auth"
	"github.com/apimount/apimount/internal/core/cache"
	"github.com/apimount/apimount/internal/core/exec"
	"github.com/apimount/apimount/internal/core/spec"

	"net/http"
	"net/http/httptest"
	"time"
)

func newTestExecutor(t *testing.T, mux *http.ServeMux) (*httptest.Server, *exec.Executor) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := exec.NewAPIClient(5*time.Second, &auth.Config{}, nil)
	c := cache.New(30*time.Second, 0)
	return srv, exec.NewExecutor(client, c, srv.URL, false)
}

func TestExecutor_GET_success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	})
	_, ex := newTestExecutor(t, mux)
	op := &spec.Operation{Method: "GET", Path: "/ping"}
	body, err := ex.ExecuteGET(context.Background(), op, nil, nil)
	require.NoError(t, err)
	assert.Contains(t, string(body), "ok")
}

func TestExecutor_WriteReturnsBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/items", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":1}`))
	})
	_, ex := newTestExecutor(t, mux)
	op := &spec.Operation{Method: "POST", Path: "/items"}
	body, err := ex.ExecuteWrite(context.Background(), op, nil, nil, []byte(`{"name":"x"}`))
	require.NoError(t, err)
	assert.Contains(t, string(body), "id")
}

// TestMiddleware_Chain verifies that middlewares are called outermost-first.
// This is tested via the executor by observing that auth is injected before transport.
func TestMiddleware_Chain_AuthInjected(t *testing.T) {
	gotAuth := ""
	mux := http.NewServeMux()
	mux.HandleFunc("/secure", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := exec.NewAPIClient(5*time.Second, &auth.Config{Bearer: "test-token"}, nil)
	c := cache.New(0, 0)
	ex := exec.NewExecutor(client, c, srv.URL, false)

	op := &spec.Operation{Method: "GET", Path: "/secure"}
	ex.ExecuteGET(context.Background(), op, nil, nil)
	assert.Equal(t, "Bearer test-token", gotAuth)
}
