package plan_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimount/apimount/internal/core/plan"
	"github.com/apimount/apimount/internal/core/spec"
)

func TestBuildTree_PathGrouping(t *testing.T) {
	data, err := spec.LoadSpec("../../../testdata/petstore.yaml")
	require.NoError(t, err)
	ps, err := spec.Parse(data, "petstore.yaml")
	require.NoError(t, err)

	root := plan.BuildTree(ps, "path")
	pet, ok := root.Children["pet"]
	require.True(t, ok, "pet/ dir missing")

	_, hasPost := pet.Children[".post"]
	_, hasPut := pet.Children[".put"]
	_, hasHelp := pet.Children[".help"]
	assert.True(t, hasPost)
	assert.True(t, hasPut)
	assert.True(t, hasHelp)

	fbs, ok := pet.Children["findByStatus"]
	require.True(t, ok, "findByStatus/ missing")
	_, hasQuery := fbs.Children[".query"]
	assert.True(t, hasQuery, "findByStatus/.query missing")

	paramDir, ok := pet.Children["{petId}"]
	require.True(t, ok, "{petId}/ missing")
	assert.True(t, paramDir.IsParamTemplate)
	assert.Equal(t, "petId", paramDir.ParamName)
}

func TestBuildTree_SchemaContent(t *testing.T) {
	data, err := spec.LoadSpec("../../../testdata/no_tags.yaml")
	require.NoError(t, err)
	ps, err := spec.Parse(data, "no_tags.yaml")
	require.NoError(t, err)

	root := plan.BuildTree(ps, "path")
	items := root.Children["items"]
	require.NotNil(t, items)

	schema, ok := items.Children[".schema"]
	require.True(t, ok)
	require.NotNil(t, schema.StaticContent)

	var parsed map[string]any
	err = json.Unmarshal(schema.StaticContent[:len(schema.StaticContent)-1], &parsed)
	require.NoError(t, err)
	props := parsed["properties"].(map[string]any)
	_, hasName := props["name"]
	assert.True(t, hasName)
}

func TestCloneWithBinding(t *testing.T) {
	data, err := spec.LoadSpec("../../../testdata/petstore.yaml")
	require.NoError(t, err)
	ps, err := spec.Parse(data, "petstore.yaml")
	require.NoError(t, err)

	root := plan.BuildTree(ps, "path")
	paramDir := root.Children["pet"].Children["{petId}"]

	cloned := plan.CloneWithBinding(paramDir, "petId", "42", root.Children["pet"])
	assert.Equal(t, "42", cloned.Name)
	assert.Equal(t, "42", cloned.PathParams["petId"])
	assert.False(t, cloned.IsParamTemplate)
	for _, child := range cloned.Children {
		if child.Type == plan.NodeTypeFile {
			assert.Equal(t, "42", child.PathParams["petId"])
		}
	}
}

func TestPrintTree(t *testing.T) {
	data, err := spec.LoadSpec("../../../testdata/petstore.yaml")
	require.NoError(t, err)
	ps, err := spec.Parse(data, "petstore.yaml")
	require.NoError(t, err)

	root := plan.BuildTree(ps, "path")
	out := plan.PrintTree(root)
	assert.Contains(t, out, "pet/")
	assert.Contains(t, out, ".data")
	assert.Contains(t, out, "{petId}/")
}
