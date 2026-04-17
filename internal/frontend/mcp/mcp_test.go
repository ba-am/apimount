package mcp

import (
	"encoding/json"
	"testing"

	"github.com/apimount/apimount/internal/core/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTool_UsesOperationID(t *testing.T) {
	op := &spec.Operation{
		OperationID: "getPetById",
		Method:      "GET",
		Path:        "/pet/{petId}",
		Summary:     "Find pet by ID",
		Parameters: []spec.Parameter{
			{Name: "petId", In: "path", Required: true, Schema: spec.Schema{Type: "integer"}},
		},
	}
	tool := buildTool(op)
	assert.Equal(t, "getPetById", tool.Name)
	assert.Contains(t, tool.Description, "GET /pet/{petId}")
	assert.Contains(t, tool.Description, "Find pet by ID")
}

func TestBuildTool_FallbackName(t *testing.T) {
	op := &spec.Operation{
		Method: "POST",
		Path:   "/pet",
	}
	tool := buildTool(op)
	assert.Equal(t, "post_pet", tool.Name)
}

func TestBuildInputSchema_IncludesParams(t *testing.T) {
	op := &spec.Operation{
		OperationID: "findPets",
		Method:      "GET",
		Path:        "/pet/findByStatus",
		Parameters: []spec.Parameter{
			{Name: "status", In: "query", Required: true, Schema: spec.Schema{Type: "string"}, Description: "Status values"},
		},
	}
	schema := buildInputSchema(op)
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(schema, &parsed))
	assert.Equal(t, "object", parsed["type"])

	props, ok := parsed["properties"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, props, "status")

	required, ok := parsed["required"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, required, "status")
}

func TestBuildInputSchema_IncludesBody(t *testing.T) {
	op := &spec.Operation{
		OperationID: "addPet",
		Method:      "POST",
		Path:        "/pet",
		RequestBody: &spec.RequestBody{Required: true},
	}
	schema := buildInputSchema(op)
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(schema, &parsed))

	props := parsed["properties"].(map[string]interface{})
	assert.Contains(t, props, "body")

	required := parsed["required"].([]interface{})
	assert.Contains(t, required, "body")
}

func TestSanitizePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/pet/{petId}", "pet_petId"},
		{"/pet/findByStatus", "pet_findByStatus"},
		{"/store/order", "store_order"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, sanitizePath(tt.input))
	}
}
