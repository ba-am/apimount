package spec

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testdataPath(name string) string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "testdata", name)
}

func TestParsePetstore(t *testing.T) {
	data, err := LoadSpec(testdataPath("petstore.yaml"))
	require.NoError(t, err)

	ps, err := Parse(data, "petstore.yaml")
	require.NoError(t, err)

	assert.Equal(t, "Swagger Petstore - OpenAPI 3.0", ps.Title)
	assert.NotEmpty(t, ps.Version)
	assert.NotEmpty(t, ps.BaseURL)
	assert.Greater(t, len(ps.Operations), 0)

	// Check we have operations with all expected methods
	methods := map[string]bool{}
	for _, op := range ps.Operations {
		methods[op.Method] = true
		assert.NotEmpty(t, op.OperationID)
		assert.NotEmpty(t, op.Path)
		assert.NotEmpty(t, op.Method)
	}
	assert.True(t, methods["GET"])
	assert.True(t, methods["POST"])
	assert.True(t, methods["PUT"])
	assert.True(t, methods["DELETE"])
}

func TestParseAuthSchemes(t *testing.T) {
	data, err := LoadSpec(testdataPath("auth_schemes.yaml"))
	require.NoError(t, err)

	ps, err := Parse(data, "auth_schemes.yaml")
	require.NoError(t, err)

	assert.Len(t, ps.AuthSchemes, 3)

	schemeMap := map[string]AuthScheme{}
	for _, s := range ps.AuthSchemes {
		schemeMap[s.Name] = s
	}

	bearer := schemeMap["bearerAuth"]
	assert.Equal(t, "http", bearer.Type)
	assert.Equal(t, "bearer", bearer.Scheme)

	apiKey := schemeMap["apiKeyAuth"]
	assert.Equal(t, "apiKey", apiKey.Type)
	assert.Equal(t, "header", apiKey.In)
	assert.Equal(t, "X-API-Key", apiKey.Param)

	basic := schemeMap["basicAuth"]
	assert.Equal(t, "http", basic.Type)
	assert.Equal(t, "basic", basic.Scheme)
}

func TestParseNoTags(t *testing.T) {
	data, err := LoadSpec(testdataPath("no_tags.yaml"))
	require.NoError(t, err)

	ps, err := Parse(data, "no_tags.yaml")
	require.NoError(t, err)

	assert.Greater(t, len(ps.Operations), 0)
	for _, op := range ps.Operations {
		assert.Empty(t, op.Tags)
	}
}

func TestRejectSwagger2(t *testing.T) {
	// Create a fake Swagger 2.0 spec
	data := []byte(`{"swagger":"2.0","info":{"title":"test","version":"1.0"},"paths":{}}`)
	_, err := Parse(data, "swagger.json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "2.0")
}

func TestGenerateOperationID(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
	}{
		{"GET", "/pets", "get_pets"},
		{"POST", "/pets/{petId}/photos", "post_pets_petid_photos"},
		{"DELETE", "/users/{userId}", "delete_users_userid"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, generateOperationID(tt.method, tt.path))
	}
}

func TestHasQueryParams(t *testing.T) {
	op := Operation{
		Parameters: []Parameter{
			{Name: "id", In: "path"},
			{Name: "limit", In: "query"},
		},
	}
	assert.True(t, HasQueryParams(op))

	op2 := Operation{
		Parameters: []Parameter{
			{Name: "id", In: "path"},
		},
	}
	assert.False(t, HasQueryParams(op2))
}
