package webdav

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"golang.org/x/net/webdav"

	"github.com/apimount/apimount/internal/core/auth"
	"github.com/apimount/apimount/internal/core/cache"
	"github.com/apimount/apimount/internal/core/exec"
	"github.com/apimount/apimount/internal/core/plan"
	"github.com/apimount/apimount/internal/core/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSetup(t *testing.T) (*httptest.Server, *plan.FSNode, *exec.Executor) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/pets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1,"name":"Rex"}]`))
	})
	upstream := httptest.NewServer(mux)
	t.Cleanup(upstream.Close)

	ps := &spec.ParsedSpec{
		Title:   "Test",
		Version: "1.0",
		Operations: []spec.Operation{
			{OperationID: "listPets", Method: "GET", Path: "/pets", Summary: "List pets"},
		},
	}
	root := plan.BuildTree(ps, "path")
	client := exec.NewAPIClient(5*time.Second, &auth.Config{}, nil)
	c := cache.New(0, 0)
	executor := exec.NewExecutor(client, c, upstream.URL, false)
	return upstream, root, executor
}

func TestWebDAV_PROPFIND_Root(t *testing.T) {
	_, root, executor := testSetup(t)

	handler := &webdav.Handler{
		FileSystem: &davFS{root: root, executor: executor},
		LockSystem: webdav.NewMemLS(),
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "PROPFIND", srv.URL+"/", nil)
	req.Header.Set("Depth", "1")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusMultiStatus, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "pets")
}

func TestWebDAV_GET_StaticFile(t *testing.T) {
	_, root, executor := testSetup(t)

	handler := &webdav.Handler{
		FileSystem: &davFS{root: root, executor: executor},
		LockSystem: webdav.NewMemLS(),
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/pets/.help")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "pets")
}

func TestWebDAV_GET_NotFound(t *testing.T) {
	_, root, executor := testSetup(t)

	handler := &webdav.Handler{
		FileSystem: &davFS{root: root, executor: executor},
		LockSystem: webdav.NewMemLS(),
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/nonexistent")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestDavFS_Stat(t *testing.T) {
	_, root, executor := testSetup(t)
	fs := &davFS{root: root, executor: executor}

	info, err := fs.Stat(context.Background(), "/")
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	_, err = fs.Stat(context.Background(), "/nonexistent")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestDavFS_ReadOnly(t *testing.T) {
	_, root, executor := testSetup(t)
	fs := &davFS{root: root, executor: executor}

	assert.ErrorIs(t, fs.Mkdir(context.Background(), "/test", 0o755), os.ErrPermission)
	assert.ErrorIs(t, fs.RemoveAll(context.Background(), "/test"), os.ErrPermission)
	assert.ErrorIs(t, fs.Rename(context.Background(), "/a", "/b"), os.ErrPermission)
}
