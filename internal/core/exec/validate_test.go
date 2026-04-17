package exec

import (
	"context"
	"testing"

	"github.com/apimount/apimount/internal/core/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateMiddleware_Disabled(t *testing.T) {
	handler := ValidateMiddleware(ValidationConfig{Enabled: false})(
		func(_ context.Context, _ *Request) (*Result, error) {
			return &Result{Status: 200}, nil
		},
	)
	res, err := handler(context.Background(), &Request{
		Op:   &spec.Operation{Method: "POST", RequestBody: &spec.RequestBody{Schema: spec.Schema{Type: "object"}}},
		Body: []byte(`not json`),
	})
	require.NoError(t, err)
	assert.Equal(t, 200, res.Status)
}

func TestValidateMiddleware_NoRequestBody(t *testing.T) {
	handler := ValidateMiddleware(ValidationConfig{Enabled: true})(
		func(_ context.Context, _ *Request) (*Result, error) {
			return &Result{Status: 200}, nil
		},
	)
	res, err := handler(context.Background(), &Request{Op: &spec.Operation{Method: "GET"}})
	require.NoError(t, err)
	assert.Equal(t, 200, res.Status)
}

func TestValidateMiddleware_ValidBody(t *testing.T) {
	handler := ValidateMiddleware(ValidationConfig{Enabled: true})(
		func(_ context.Context, _ *Request) (*Result, error) {
			return &Result{Status: 201}, nil
		},
	)
	res, err := handler(context.Background(), &Request{
		Op: &spec.Operation{
			OperationID: "addPet",
			Method:      "POST",
			RequestBody: &spec.RequestBody{
				Schema: spec.Schema{
					Type:     "object",
					Required: []string{"name"},
					Properties: map[string]spec.Schema{
						"name":      {Type: "string"},
						"photoUrls": {Type: "array", Items: &spec.Schema{Type: "string"}},
					},
				},
			},
		},
		Body: []byte(`{"name":"Rex","photoUrls":["http://img.example.com/rex.jpg"]}`),
	})
	require.NoError(t, err)
	assert.Equal(t, 201, res.Status)
}

func TestValidateMiddleware_MissingRequiredField(t *testing.T) {
	handler := ValidateMiddleware(ValidationConfig{Enabled: true})(
		func(_ context.Context, _ *Request) (*Result, error) {
			return &Result{Status: 201}, nil
		},
	)
	_, err := handler(context.Background(), &Request{
		Op: &spec.Operation{
			OperationID: "addPet",
			Method:      "POST",
			RequestBody: &spec.RequestBody{
				Schema: spec.Schema{
					Type:     "object",
					Required: []string{"name", "photoUrls"},
					Properties: map[string]spec.Schema{
						"name":      {Type: "string"},
						"photoUrls": {Type: "array"},
					},
				},
			},
		},
		Body: []byte(`{"photoUrls":[]}`),
	})
	require.Error(t, err)
	var valErr *ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, "addPet", valErr.OperationID)
	assert.Len(t, valErr.Violations, 1)
	assert.Equal(t, "/name", valErr.Violations[0].Path)
	assert.Contains(t, valErr.Violations[0].Reason, "required")
}

func TestValidateMiddleware_WrongType(t *testing.T) {
	handler := ValidateMiddleware(ValidationConfig{Enabled: true})(
		func(_ context.Context, _ *Request) (*Result, error) {
			return &Result{Status: 201}, nil
		},
	)
	_, err := handler(context.Background(), &Request{
		Op: &spec.Operation{
			OperationID: "addPet",
			Method:      "POST",
			RequestBody: &spec.RequestBody{
				Schema: spec.Schema{
					Type: "object",
					Properties: map[string]spec.Schema{
						"name":      {Type: "string"},
						"photoUrls": {Type: "array", Items: &spec.Schema{Type: "string"}},
					},
				},
			},
		},
		Body: []byte(`{"name":"Rex","photoUrls":"not-an-array"}`),
	})
	require.Error(t, err)
	var valErr *ValidationError
	require.ErrorAs(t, err, &valErr)
	found := false
	for _, v := range valErr.Violations {
		if v.Path == "/photoUrls" {
			found = true
			assert.Contains(t, v.Reason, "expected array")
		}
	}
	assert.True(t, found, "expected violation for /photoUrls")
}

func TestValidateMiddleware_InvalidJSON(t *testing.T) {
	handler := ValidateMiddleware(ValidationConfig{Enabled: true})(
		func(_ context.Context, _ *Request) (*Result, error) {
			return &Result{Status: 201}, nil
		},
	)
	_, err := handler(context.Background(), &Request{
		Op: &spec.Operation{
			OperationID: "addPet",
			Method:      "POST",
			RequestBody: &spec.RequestBody{
				Schema: spec.Schema{Type: "object"},
			},
		},
		Body: []byte(`{not valid`),
	})
	require.Error(t, err)
	var valErr *ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Contains(t, valErr.Violations[0].Reason, "invalid JSON")
}

func TestValidateMiddleware_EmptyBody(t *testing.T) {
	handler := ValidateMiddleware(ValidationConfig{Enabled: true})(
		func(_ context.Context, _ *Request) (*Result, error) {
			return &Result{Status: 200}, nil
		},
	)
	res, err := handler(context.Background(), &Request{
		Op: &spec.Operation{
			Method:      "POST",
			RequestBody: &spec.RequestBody{Schema: spec.Schema{Type: "object"}},
		},
		Body: nil,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, res.Status)
}

func TestValidateMiddleware_NestedObject(t *testing.T) {
	handler := ValidateMiddleware(ValidationConfig{Enabled: true})(
		func(_ context.Context, _ *Request) (*Result, error) {
			return &Result{Status: 201}, nil
		},
	)
	_, err := handler(context.Background(), &Request{
		Op: &spec.Operation{
			OperationID: "createOrder",
			Method:      "POST",
			RequestBody: &spec.RequestBody{
				Schema: spec.Schema{
					Type: "object",
					Properties: map[string]spec.Schema{
						"address": {
							Type:     "object",
							Required: []string{"city"},
							Properties: map[string]spec.Schema{
								"city":   {Type: "string"},
								"street": {Type: "string"},
							},
						},
					},
				},
			},
		},
		Body: []byte(`{"address":{"street":"Main St"}}`),
	})
	require.Error(t, err)
	var valErr *ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Len(t, valErr.Violations, 1)
	assert.Equal(t, "/address/city", valErr.Violations[0].Path)
}

func TestValidateMiddleware_ArrayItems(t *testing.T) {
	handler := ValidateMiddleware(ValidationConfig{Enabled: true})(
		func(_ context.Context, _ *Request) (*Result, error) {
			return &Result{Status: 201}, nil
		},
	)
	_, err := handler(context.Background(), &Request{
		Op: &spec.Operation{
			OperationID: "addTags",
			Method:      "POST",
			RequestBody: &spec.RequestBody{
				Schema: spec.Schema{
					Type:  "array",
					Items: &spec.Schema{Type: "string"},
				},
			},
		},
		Body: []byte(`["valid", 123]`),
	})
	require.Error(t, err)
	var valErr *ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Len(t, valErr.Violations, 1)
	assert.Contains(t, valErr.Violations[0].Path, "[1]")
}

func TestValidateValue_Enum(t *testing.T) {
	schema := &spec.Schema{
		Type: "string",
		Enum: []interface{}{"available", "pending", "sold"},
	}
	violations := validateValue("available", schema, "/status")
	assert.Empty(t, violations)

	violations = validateValue("unknown", schema, "/status")
	assert.Len(t, violations, 1)
	assert.Contains(t, violations[0].Reason, "not in enum")
}
