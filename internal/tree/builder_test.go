package tree

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/apimount/apimount/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testdataPath(name string) string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "testdata", name)
}

func loadSpec(t *testing.T, name string) *spec.ParsedSpec {
	t.Helper()
	data, err := spec.LoadSpec(testdataPath(name))
	require.NoError(t, err)
	ps, err := spec.Parse(data, name)
	require.NoError(t, err)
	return ps
}

func TestBuildTreePetstore(t *testing.T) {
	ps := loadSpec(t, "petstore.yaml")
	root := BuildTree(ps, "tags")

	// Root should have tag-based directories
	assert.NotEmpty(t, root.Children)

	// Every dir should have a .help file
	var checkHelp func(n *FSNode)
	checkHelp = func(n *FSNode) {
		if n.Type == NodeTypeDir && n.Name != "/" {
			_, ok := n.Children[".help"]
			assert.True(t, ok, "dir %q missing .help", n.Name)
		}
		for _, child := range n.Children {
			if child.Type == NodeTypeDir {
				checkHelp(child)
			}
		}
	}
	checkHelp(root)
}

func TestBuildTreeNoTags(t *testing.T) {
	ps := loadSpec(t, "no_tags.yaml")
	root := BuildTree(ps, "tags")

	// All ops have no tags → should be in "untagged"
	untagged, ok := root.Children["untagged"]
	require.True(t, ok, "expected 'untagged' dir")
	assert.NotEmpty(t, untagged.Children)
}

func TestBuildTreePathGrouping(t *testing.T) {
	ps := loadSpec(t, "no_tags.yaml")
	root := BuildTree(ps, "path")

	// Should have 'items' dir directly under root
	items, ok := root.Children["items"]
	require.True(t, ok)
	assert.NotEmpty(t, items.Children)

	// items/.data should be GET
	dataFile, ok := items.Children[".data"]
	require.True(t, ok)
	assert.Equal(t, FileRoleGET, dataFile.Role)

	// items/.post should be POST
	postFile, ok := items.Children[".post"]
	require.True(t, ok)
	assert.Equal(t, FileRolePost, postFile.Role)
}

func TestBuildTreeParamDir(t *testing.T) {
	ps := loadSpec(t, "no_tags.yaml")
	root := BuildTree(ps, "path")

	items, ok := root.Children["items"]
	require.True(t, ok)

	// Should have {id} param dir
	paramDir, ok := items.Children["{id}"]
	require.True(t, ok)
	assert.True(t, paramDir.IsParamTemplate)
	assert.Equal(t, "id", paramDir.ParamName)

	// {id} should have .data, .put, .delete
	_, hasData := paramDir.Children[".data"]
	_, hasPut := paramDir.Children[".put"]
	_, hasDelete := paramDir.Children[".delete"]
	assert.True(t, hasData)
	assert.True(t, hasPut)
	assert.True(t, hasDelete)
}

func TestCloneWithBinding(t *testing.T) {
	ps := loadSpec(t, "no_tags.yaml")
	root := BuildTree(ps, "path")

	items := root.Children["items"]
	paramDir := items.Children["{id}"]
	require.NotNil(t, paramDir)

	// Clone with value "42"
	cloned := CloneWithBinding(paramDir, "id", "42", items)

	assert.Equal(t, "42", cloned.Name)
	assert.Equal(t, "42", cloned.PathParams["id"])
	assert.False(t, cloned.IsParamTemplate)

	// Children should have path params too
	for _, child := range cloned.Children {
		if child.Type == NodeTypeFile {
			assert.Equal(t, "42", child.PathParams["id"])
		}
	}
}

func TestQueryFileAddedForQueryParams(t *testing.T) {
	ps := loadSpec(t, "no_tags.yaml")
	root := BuildTree(ps, "path")

	// /items GET has 'limit' query param → should have .query file
	items := root.Children["items"]
	_, hasQuery := items.Children[".query"]
	assert.True(t, hasQuery, "expected .query file for operation with query params")
}

func TestSchemaFileForRequestBody(t *testing.T) {
	ps := loadSpec(t, "no_tags.yaml")
	root := BuildTree(ps, "path")

	items := root.Children["items"]
	// POST /items has request body → .schema
	schema, ok := items.Children[".schema"]
	require.True(t, ok)
	assert.Equal(t, FileRoleSchema, schema.Role)
}

func TestSplitPath(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"/pets/{petId}/photos", []string{"pets", "{petId}", "photos"}},
		{"/pets", []string{"pets"}},
		{"/", []string{}},
		{"", []string{}},
	}
	for _, tt := range tests {
		got := splitPath(tt.input)
		assert.Equal(t, tt.want, got, "splitPath(%q)", tt.input)
	}
}

func TestIsPathParam(t *testing.T) {
	assert.True(t, isPathParam("{petId}"))
	assert.True(t, isPathParam("{id}"))
	assert.False(t, isPathParam("pets"))
	assert.False(t, isPathParam("findByStatus"))
}

func TestExtractParamName(t *testing.T) {
	assert.Equal(t, "petId", extractParamName("{petId}"))
	assert.Equal(t, "id", extractParamName("{id}"))
}
