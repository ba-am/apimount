package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"
	"time"

	"github.com/apimount/apimount/internal/auth"
	"github.com/apimount/apimount/internal/cache"
	"github.com/apimount/apimount/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestExecutor(t *testing.T, server *httptest.Server) *Executor {
	t.Helper()
	client := NewAPIClient(5*time.Second, &auth.Config{}, nil)
	c := cache.New(30*time.Second, 0)
	return NewExecutor(client, c, server.URL, true)
}

func TestExecuteGET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/pets", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`[{"id":1,"name":"Fido"}]`))
	}))
	defer srv.Close()

	exec := newTestExecutor(t, srv)
	op := &spec.Operation{Method: "GET", Path: "/pets"}
	body, errno, err := exec.ExecuteGET(context.Background(), op, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, syscall.Errno(0), errno)
	assert.Contains(t, string(body), "Fido")
}

func TestExecuteGETWithPathParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/pets/42", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":42,"name":"Rex"}`))
	}))
	defer srv.Close()

	exec := newTestExecutor(t, srv)
	op := &spec.Operation{Method: "GET", Path: "/pets/{petId}"}
	body, _, err := exec.ExecuteGET(context.Background(), op, map[string]string{"petId": "42"}, nil)
	require.NoError(t, err)
	assert.Contains(t, string(body), "Rex")
}

func TestExecuteGETWithQueryParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "available", r.URL.Query().Get("status"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	exec := newTestExecutor(t, srv)
	op := &spec.Operation{Method: "GET", Path: "/pets"}
	_, _, err := exec.ExecuteGET(context.Background(), op, nil, map[string]string{"status": "available"})
	require.NoError(t, err)
}

func TestExecutePOST(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		w.Write([]byte(`{"id":99,"name":"New"}`))
	}))
	defer srv.Close()

	exec := newTestExecutor(t, srv)
	op := &spec.Operation{Method: "POST", Path: "/pets"}
	body, errno, err := exec.ExecuteWrite(context.Background(), op, nil, nil, []byte(`{"name":"New"}`))
	require.NoError(t, err)
	assert.Equal(t, syscall.Errno(0), errno)
	assert.Contains(t, string(body), "99")
}

func TestExecuteGET401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	exec := newTestExecutor(t, srv)
	op := &spec.Operation{Method: "GET", Path: "/secret"}
	body, errno, _ := exec.ExecuteGET(context.Background(), op, nil, nil)
	assert.Equal(t, syscall.EACCES, errno)
	assert.Contains(t, string(body), "401")
}

func TestCacheHit(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"count":1}`))
	}))
	defer srv.Close()

	exec := newTestExecutor(t, srv)
	op := &spec.Operation{Method: "GET", Path: "/pets"}

	_, _, err := exec.ExecuteGET(context.Background(), op, nil, nil)
	require.NoError(t, err)
	_, _, err = exec.ExecuteGET(context.Background(), op, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, callCount, "second call should hit cache")
}

func TestBearerAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer mytoken", r.Header.Get("Authorization"))
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := NewAPIClient(5*time.Second, &auth.Config{Bearer: "mytoken"}, nil)
	c := cache.New(30*time.Second, 0)
	exec := NewExecutor(client, c, srv.URL, false)
	op := &spec.Operation{Method: "GET", Path: "/protected"}
	_, _, err := exec.ExecuteGET(context.Background(), op, nil, nil)
	require.NoError(t, err)
}
