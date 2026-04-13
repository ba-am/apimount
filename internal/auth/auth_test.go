package auth

import (
	"encoding/base64"
	"testing"

	"github.com/apimount/apimount/internal/spec"
	"github.com/stretchr/testify/assert"
)

func TestBearerAuth(t *testing.T) {
	inj := NewInjector(&Config{Bearer: "mytoken"}, nil)
	headers := map[string]string{}
	queryParams := map[string]string{}
	inj.Apply(nil, headers, queryParams)
	assert.Equal(t, "Bearer mytoken", headers["Authorization"])
	assert.Empty(t, queryParams)
}

func TestBasicAuth(t *testing.T) {
	inj := NewInjector(&Config{Basic: "user:pass"}, nil)
	headers := map[string]string{}
	queryParams := map[string]string{}
	inj.Apply(nil, headers, queryParams)
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	assert.Equal(t, expected, headers["Authorization"])
}

func TestAPIKeyInHeader(t *testing.T) {
	schemes := []spec.AuthScheme{
		{Name: "apiKeyAuth", Type: "apiKey", In: "header", Param: "X-API-Key"},
	}
	inj := NewInjector(&Config{APIKey: "secret123"}, schemes)
	headers := map[string]string{}
	queryParams := map[string]string{}
	opSec := []spec.SecurityReq{{"apiKeyAuth": {}}}
	inj.Apply(opSec, headers, queryParams)
	assert.Equal(t, "secret123", headers["X-API-Key"])
}

func TestAPIKeyInQuery(t *testing.T) {
	schemes := []spec.AuthScheme{
		{Name: "queryKey", Type: "apiKey", In: "query", Param: "api_key"},
	}
	inj := NewInjector(&Config{APIKey: "mykey"}, schemes)
	headers := map[string]string{}
	queryParams := map[string]string{}
	opSec := []spec.SecurityReq{{"queryKey": {}}}
	inj.Apply(opSec, headers, queryParams)
	assert.Equal(t, "mykey", queryParams["api_key"])
	assert.Empty(t, headers["Authorization"])
}

func TestAPIKeyParamOverride(t *testing.T) {
	inj := NewInjector(&Config{APIKey: "override", APIKeyParam: "My-Custom-Key"}, nil)
	headers := map[string]string{}
	queryParams := map[string]string{}
	inj.ApplyDirect(headers, queryParams)
	assert.Equal(t, "override", headers["My-Custom-Key"])
}

func TestNoCredentials(t *testing.T) {
	inj := NewInjector(&Config{}, nil)
	headers := map[string]string{}
	queryParams := map[string]string{}
	inj.Apply(nil, headers, queryParams)
	assert.Empty(t, headers)
	assert.Empty(t, queryParams)
}

func TestBearerTakesPrecedenceOverBasic(t *testing.T) {
	inj := NewInjector(&Config{Bearer: "tok", Basic: "u:p"}, nil)
	headers := map[string]string{}
	queryParams := map[string]string{}
	inj.Apply(nil, headers, queryParams)
	assert.Equal(t, "Bearer tok", headers["Authorization"])
}
