package spec_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimount/apimount/internal/core/spec"
)

func TestParse_Petstore(t *testing.T) {
	data, err := spec.LoadSpec("../../../testdata/petstore.yaml")
	require.NoError(t, err)
	ps, err := spec.Parse(data, "petstore.yaml")
	require.NoError(t, err)

	assert.NotEmpty(t, ps.Title)
	assert.NotEmpty(t, ps.Operations)
	assert.NotEmpty(t, ps.AuthSchemes)
}

func TestParse_OperationFields(t *testing.T) {
	data, err := spec.LoadSpec("../../../testdata/petstore.yaml")
	require.NoError(t, err)
	ps, err := spec.Parse(data, "petstore.yaml")
	require.NoError(t, err)

	var getById *spec.Operation
	for i := range ps.Operations {
		op := &ps.Operations[i]
		if op.Method == "GET" && op.Path == "/pet/{petId}" {
			getById = op
			break
		}
	}
	require.NotNil(t, getById, "GET /pet/{petId} not found")
	assert.Equal(t, "getPetById", getById.OperationID)
	assert.NotEmpty(t, getById.Summary)
	assert.NotEmpty(t, getById.Tags)

	found := false
	for _, p := range getById.Parameters {
		if p.Name == "petId" && p.In == "path" {
			found = true
			assert.True(t, p.Required)
		}
	}
	assert.True(t, found, "petId path param not found")
}

func TestParse_RequestBody(t *testing.T) {
	data, err := spec.LoadSpec("../../../testdata/no_tags.yaml")
	require.NoError(t, err)
	ps, err := spec.Parse(data, "no_tags.yaml")
	require.NoError(t, err)

	var createOp *spec.Operation
	for i := range ps.Operations {
		if ps.Operations[i].OperationID == "createItem" {
			createOp = &ps.Operations[i]
			break
		}
	}
	require.NotNil(t, createOp)
	require.NotNil(t, createOp.RequestBody)
	assert.True(t, createOp.RequestBody.Required)
	assert.Equal(t, "application/json", createOp.RequestBody.ContentType)
	_, hasName := createOp.RequestBody.Schema.Properties["name"]
	assert.True(t, hasName, "schema should have 'name' property")
}

func TestParse_Swagger2Rejected(t *testing.T) {
	swagger := []byte(`swagger: "2.0"\ninfo:\n  title: Old`)
	_, err := spec.Parse(swagger, "old.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2.0")
}

func TestHasQueryParams(t *testing.T) {
	op := spec.Operation{
		Parameters: []spec.Parameter{
			{Name: "status", In: "query"},
			{Name: "petId", In: "path"},
		},
	}
	assert.True(t, spec.HasQueryParams(op))

	op2 := spec.Operation{
		Parameters: []spec.Parameter{{Name: "petId", In: "path"}},
	}
	assert.False(t, spec.HasQueryParams(op2))
}
