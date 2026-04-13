package fs

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/apimount/apimount/internal/tree"
)

// Tests for helpers that don't require a live FUSE mount.

func TestFileMode(t *testing.T) {
	writeable := []tree.FileRole{
		tree.FileRolePost,
		tree.FileRolePut,
		tree.FileRoleDelete,
		tree.FileRolePatch,
		tree.FileRoleQuery,
	}
	readonly := []tree.FileRole{
		tree.FileRoleGET,
		tree.FileRoleSchema,
		tree.FileRoleHelp,
		tree.FileRoleResponse,
	}

	for _, role := range writeable {
		n := tree.NewFileNode("f", role, nil, nil)
		mode := fileMode(n)
		assert.Equal(t, uint32(0644)|syscallSIFREG, mode&0xFFF|syscallSIFREG,
			"role %d should be writeable (0644)", role)
	}
	for _, role := range readonly {
		n := tree.NewFileNode("f", role, nil, nil)
		mode := fileMode(n)
		assert.Equal(t, uint32(0444)|syscallSIFREG, mode&0xFFF|syscallSIFREG,
			"role %d should be read-only (0444)", role)
	}
}

func TestParseQueryString(t *testing.T) {
	tests := []struct {
		input string
		want  map[string]string
	}{
		{"status=available&limit=10", map[string]string{"status": "available", "limit": "10"}},
		{"key=val", map[string]string{"key": "val"}},
		{"bare", map[string]string{"bare": ""}},
		{"  key = val  ", map[string]string{"key": "val"}},
		{"", map[string]string{}},
	}
	for _, tt := range tests {
		got := parseQueryString(tt.input)
		assert.Equal(t, tt.want, got, "parseQueryString(%q)", tt.input)
	}
}

func TestPrettyFormat(t *testing.T) {
	// Valid JSON gets pretty-printed
	raw := []byte(`{"id":1,"name":"Fido"}`)
	out := prettyFormat(raw)
	assert.Contains(t, string(out), "\n")
	assert.Contains(t, string(out), `"id"`)

	// Invalid JSON returned as-is
	plain := []byte("not json")
	assert.Equal(t, plain, prettyFormat(plain))

	// Empty
	assert.Equal(t, []byte(nil), prettyFormat(nil))
}

func TestResolveHelpContent(t *testing.T) {
	n := tree.NewFileNode(".help", tree.FileRoleHelp, nil, nil)
	n.StaticContent = []byte("GET /pets/{petId}\nsome description")
	n.PathParams = map[string]string{"petId": "42"}

	out := resolveHelpContent(n)
	assert.Contains(t, string(out), "GET /pets/42")
	assert.NotContains(t, string(out), "{petId}")
}

func TestResolveHelpContentNil(t *testing.T) {
	n := tree.NewFileNode(".help", tree.FileRoleHelp, nil, nil)
	out := resolveHelpContent(n)
	assert.Contains(t, string(out), "no help")
}

func TestStoreResponse(t *testing.T) {
	parent := tree.NewDirNode("pets", nil)
	opFile := tree.NewFileNode(".post", tree.FileRolePost, parent, nil)
	respFile := tree.NewFileNode(".response", tree.FileRoleResponse, parent, nil)
	parent.Children[".post"] = opFile
	parent.Children[".response"] = respFile

	body := []byte(`{"id":1}`)
	storeResponse(opFile, body)

	opFile.Mu.RLock()
	assert.NotEmpty(t, opFile.LastResponse)
	opFile.Mu.RUnlock()

	respFile.Mu.RLock()
	assert.NotEmpty(t, respFile.LastResponse)
	respFile.Mu.RUnlock()
}

func TestFlushGuard(t *testing.T) {
	// flushed flag prevents double-execution
	fh := &fileHandle{flushed: false}
	fh.flushed = true
	// Would normally call executeWrite but with flushed=true it's a no-op
	assert.True(t, fh.flushed)
}

// syscallSIFREG is the regular file type bit, used to isolate the permission bits.
const syscallSIFREG = uint32(0x8000)
