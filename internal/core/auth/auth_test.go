package auth_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/apimount/apimount/internal/core/auth"
	"github.com/apimount/apimount/internal/core/spec"
)

func TestBearerAuth(t *testing.T) {
	inj := auth.NewInjector(&auth.Config{Bearer: "mytoken"}, nil)
	headers, queryParams := map[string]string{}, map[string]string{}
	inj.Apply(nil, headers, queryParams)
	assert.Equal(t, "Bearer mytoken", headers["Authorization"])
	assert.Empty(t, queryParams)
}

func TestBasicAuth(t *testing.T) {
	inj := auth.NewInjector(&auth.Config{Basic: "user:pass"}, nil)
	headers, queryParams := map[string]string{}, map[string]string{}
	inj.Apply(nil, headers, queryParams)
	assert.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")), headers["Authorization"])
}

func TestAPIKeyInHeader(t *testing.T) {
	schemes := []spec.AuthScheme{{Name: "apiKeyAuth", Type: "apiKey", In: "header", Param: "X-API-Key"}}
	inj := auth.NewInjector(&auth.Config{APIKey: "secret123"}, schemes)
	headers, queryParams := map[string]string{}, map[string]string{}
	inj.Apply([]spec.SecurityReq{{"apiKeyAuth": {}}}, headers, queryParams)
	assert.Equal(t, "secret123", headers["X-API-Key"])
}

func TestAPIKeyInQuery(t *testing.T) {
	schemes := []spec.AuthScheme{{Name: "queryKey", Type: "apiKey", In: "query", Param: "api_key"}}
	inj := auth.NewInjector(&auth.Config{APIKey: "mykey"}, schemes)
	headers, queryParams := map[string]string{}, map[string]string{}
	inj.Apply([]spec.SecurityReq{{"queryKey": {}}}, headers, queryParams)
	assert.Equal(t, "mykey", queryParams["api_key"])
	assert.Empty(t, headers["Authorization"])
}

func TestNoCredentials(t *testing.T) {
	inj := auth.NewInjector(&auth.Config{}, nil)
	headers, queryParams := map[string]string{}, map[string]string{}
	inj.Apply(nil, headers, queryParams)
	assert.Empty(t, headers)
	assert.Empty(t, queryParams)
}

func TestBearerTakesPrecedence(t *testing.T) {
	inj := auth.NewInjector(&auth.Config{Bearer: "tok", Basic: "u:p"}, nil)
	headers, queryParams := map[string]string{}, map[string]string{}
	inj.Apply(nil, headers, queryParams)
	assert.Equal(t, "Bearer tok", headers["Authorization"])
}
