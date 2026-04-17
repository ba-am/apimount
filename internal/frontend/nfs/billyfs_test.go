package nfs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	gonfs "github.com/willscott/go-nfs"

	"github.com/apimount/apimount/internal/core/auth"
	"github.com/apimount/apimount/internal/core/cache"
	"github.com/apimount/apimount/internal/core/exec"
	"github.com/apimount/apimount/internal/core/plan"
	"github.com/apimount/apimount/internal/core/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSetup(t *testing.T) (*plan.FSNode, *billyFS) {
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
	bfs := newBillyFS(root, executor, upstream.URL)
	return root, bfs
}

func TestBillyFS_Stat_Root(t *testing.T) {
	_, bfs := testSetup(t)
	info, err := bfs.Stat("/")
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestBillyFS_Stat_NotExist(t *testing.T) {
	_, bfs := testSetup(t)
	_, err := bfs.Stat("/nonexistent")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestBillyFS_ReadDir_Root(t *testing.T) {
	_, bfs := testSetup(t)
	infos, err := bfs.ReadDir("/")
	require.NoError(t, err)
	assert.NotEmpty(t, infos)

	names := make(map[string]bool)
	for _, info := range infos {
		names[info.Name()] = true
	}
	assert.True(t, names["pets"], "expected 'pets' directory")
}

func TestBillyFS_Open_StaticContent(t *testing.T) {
	_, bfs := testSetup(t)
	f, err := bfs.Open("/pets/.help")
	require.NoError(t, err)
	defer f.Close()

	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Contains(t, string(data), "pets")
}

func TestBillyFS_ReadOnly(t *testing.T) {
	_, bfs := testSetup(t)
	_, err := bfs.Create("/test")
	assert.ErrorIs(t, err, os.ErrPermission)

	assert.ErrorIs(t, bfs.Remove("/test"), os.ErrPermission)
	assert.ErrorIs(t, bfs.Rename("/a", "/b"), os.ErrPermission)
	assert.ErrorIs(t, bfs.MkdirAll("/test", 0o755), os.ErrPermission)
}

func TestBillyFS_Chroot(t *testing.T) {
	_, bfs := testSetup(t)
	sub, err := bfs.Chroot("/pets")
	require.NoError(t, err)

	info, err := sub.Stat("/")
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNFSHandler_Mount(t *testing.T) {
	_, bfs := testSetup(t)
	h := &nfsHandler{fs: bfs}

	status, fs, flavors := h.Mount(context.Background(), nil, gonfs.MountRequest{})
	assert.Equal(t, gonfs.MountStatusOk, status)
	assert.NotNil(t, fs)
	assert.NotEmpty(t, flavors)
}

func TestNFSHandler_HandleRoundTrip(t *testing.T) {
	_, bfs := testSetup(t)
	h := &nfsHandler{fs: bfs}

	handle := h.ToHandle(bfs, []string{"pets"})
	assert.NotEmpty(t, handle)

	fs, path, err := h.FromHandle(handle)
	require.NoError(t, err)
	assert.NotNil(t, fs)
	assert.Equal(t, []string{"pets"}, path)
}
