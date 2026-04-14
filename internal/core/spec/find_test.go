package spec_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimount/apimount/internal/core/spec"
)

func loadPetstore(t *testing.T) *spec.ParsedSpec {
	t.Helper()
	data, err := spec.LoadSpec("../../../testdata/petstore.yaml")
	require.NoError(t, err)
	ps, err := spec.Parse(data, "petstore.yaml")
	require.NoError(t, err)
	return ps
}

func TestFindOperation_LiteralPath(t *testing.T) {
	ps := loadPetstore(t)
	op, params, err := spec.FindOperation(ps, "GET", "/pet/findByStatus")
	require.NoError(t, err)
	assert.Equal(t, "GET", op.Method)
	assert.Equal(t, "/pet/findByStatus", op.Path)
	assert.Empty(t, params)
}

func TestFindOperation_TemplatedPath(t *testing.T) {
	ps := loadPetstore(t)
	op, params, err := spec.FindOperation(ps, "GET", "/pet/42")
	require.NoError(t, err)
	assert.Equal(t, "/pet/{petId}", op.Path)
	assert.Equal(t, "42", params["petId"])
}

func TestFindOperation_PrefersLiteralsOverTemplate(t *testing.T) {
	ps := loadPetstore(t)
	// "/pet/findByStatus" is a literal path; "/pet/{petId}" would also match a
	// single-segment URL. The literal match must win.
	op, params, err := spec.FindOperation(ps, "GET", "/pet/findByStatus")
	require.NoError(t, err)
	assert.Equal(t, "/pet/findByStatus", op.Path)
	assert.Empty(t, params)
}

func TestFindOperation_MethodCaseInsensitive(t *testing.T) {
	ps := loadPetstore(t)
	_, _, err := spec.FindOperation(ps, "get", "/pet/findByStatus")
	require.NoError(t, err)
}

func TestFindOperation_NoMatch(t *testing.T) {
	ps := loadPetstore(t)
	_, _, err := spec.FindOperation(ps, "GET", "/nonexistent/path")
	require.Error(t, err)
}

func TestFindOperation_WrongMethod(t *testing.T) {
	ps := loadPetstore(t)
	// /pet supports POST/PUT but not GET.
	_, _, err := spec.FindOperation(ps, "TRACE", "/pet")
	require.Error(t, err)
}
