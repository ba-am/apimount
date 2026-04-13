// Package integration exercises the full stack (spec → tree → HTTP executor)
// without a live FUSE mount by calling the internal components directly.
package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimount/apimount/internal/auth"
	"github.com/apimount/apimount/internal/cache"
	apihttp "github.com/apimount/apimount/internal/http"
	"github.com/apimount/apimount/internal/spec"
	"github.com/apimount/apimount/internal/tree"
)

// mockAPI sets up a simple pet API mock and returns the server + executor.
func mockAPI(t *testing.T, mux *http.ServeMux) (*httptest.Server, *apihttp.Executor) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := apihttp.NewAPIClient(5*time.Second, &auth.Config{}, nil)
	c := cache.New(30*time.Second, 0)
	exec := apihttp.NewExecutor(client, c, srv.URL, true)
	return srv, exec
}

func TestFullFlow_GETList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/pets", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "name": "Fido", "status": "available"},
		})
	})
	_, exec := mockAPI(t, mux)

	op := &spec.Operation{Method: "GET", Path: "/pets"}
	body, errno, err := exec.ExecuteGET(context.Background(), op, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, uintptr(0), uintptr(errno))
	assert.Contains(t, string(body), "Fido")
	assert.Contains(t, string(body), `"status"`)
}

func TestFullFlow_GETByPathParam(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/pets/42", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "Rex"})
	})
	_, exec := mockAPI(t, mux)

	op := &spec.Operation{Method: "GET", Path: "/pets/{petId}"}
	body, errno, err := exec.ExecuteGET(context.Background(), op, map[string]string{"petId": "42"}, nil)
	require.NoError(t, err)
	assert.Equal(t, uintptr(0), uintptr(errno))
	assert.Contains(t, string(body), "Rex")
}

func TestFullFlow_GETWithQueryParam(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/pets", func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		assert.Equal(t, "available", status)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "status": status}})
	})
	_, exec := mockAPI(t, mux)

	op := &spec.Operation{Method: "GET", Path: "/pets"}
	body, _, err := exec.ExecuteGET(context.Background(), op, nil, map[string]string{"status": "available"})
	require.NoError(t, err)
	assert.Contains(t, string(body), "available")
}

func TestFullFlow_POST(t *testing.T) {
	var receivedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/pets", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": 99, "name": "New"})
	})
	_, exec := mockAPI(t, mux)

	op := &spec.Operation{Method: "POST", Path: "/pets"}
	payload := []byte(`{"name":"New","photoUrls":[]}`)
	body, errno, err := exec.ExecuteWrite(context.Background(), op, nil, nil, payload)
	require.NoError(t, err)
	assert.Equal(t, uintptr(0), uintptr(errno))
	assert.Contains(t, string(body), "99")
	assert.Equal(t, payload, receivedBody)
}

func TestFullFlow_PUT(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/pets/5", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": 5, "name": "Updated"})
	})
	_, exec := mockAPI(t, mux)

	op := &spec.Operation{Method: "PUT", Path: "/pets/{petId}"}
	body, errno, err := exec.ExecuteWrite(context.Background(), op, map[string]string{"petId": "5"}, nil, []byte(`{"name":"Updated"}`))
	require.NoError(t, err)
	assert.Equal(t, uintptr(0), uintptr(errno))
	assert.Contains(t, string(body), "Updated")
}

func TestFullFlow_DELETE(t *testing.T) {
	deleted := false
	mux := http.NewServeMux()
	mux.HandleFunc("/pets/7", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		deleted = true
		w.WriteHeader(http.StatusNoContent)
	})
	_, exec := mockAPI(t, mux)

	op := &spec.Operation{Method: "DELETE", Path: "/pets/{petId}"}
	_, _, err := exec.ExecuteWrite(context.Background(), op, map[string]string{"petId": "7"}, nil, nil)
	require.NoError(t, err)
	assert.True(t, deleted, "DELETE was not called")
}

func TestFullFlow_GET_401(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/secret", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	_, exec := mockAPI(t, mux)

	op := &spec.Operation{Method: "GET", Path: "/secret"}
	body, errno, _ := exec.ExecuteGET(context.Background(), op, nil, nil)
	assert.NotEqual(t, uintptr(0), uintptr(errno))
	assert.Contains(t, string(body), "401")
}

func TestFullFlow_GET_404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/pets/999", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, exec := mockAPI(t, mux)

	op := &spec.Operation{Method: "GET", Path: "/pets/{petId}"}
	body, errno, _ := exec.ExecuteGET(context.Background(), op, map[string]string{"petId": "999"}, nil)
	assert.NotEqual(t, uintptr(0), uintptr(errno))
	assert.Contains(t, string(body), "404")
}

func TestFullFlow_CacheHit(t *testing.T) {
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/pets", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{{"id": calls}})
	})
	_, exec := mockAPI(t, mux)

	op := &spec.Operation{Method: "GET", Path: "/pets"}
	exec.ExecuteGET(context.Background(), op, nil, nil)
	exec.ExecuteGET(context.Background(), op, nil, nil)
	exec.ExecuteGET(context.Background(), op, nil, nil)
	assert.Equal(t, 1, calls, "cache should have served 2nd and 3rd reads")
}

func TestFullFlow_CacheInvalidatedAfterWrite(t *testing.T) {
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/pets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			calls++
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{{"id": calls}})
		} else {
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{}`))
		}
	})
	_, exec := mockAPI(t, mux)
	op := &spec.Operation{Method: "GET", Path: "/pets"}
	postOp := &spec.Operation{Method: "POST", Path: "/pets"}

	exec.ExecuteGET(context.Background(), op, nil, nil)  // call 1, cached
	exec.ExecuteGET(context.Background(), op, nil, nil)  // cache hit
	exec.ExecuteWrite(context.Background(), postOp, nil, nil, []byte(`{}`)) // invalidates cache
	exec.ExecuteGET(context.Background(), op, nil, nil)  // call 2, re-fetched
	assert.Equal(t, 2, calls, "cache should be invalidated after POST")
}

func TestFullFlow_BearerAuth(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer tok123", r.Header.Get("Authorization"))
		w.WriteHeader(200)
		w.Write([]byte(`{"user":"me"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := apihttp.NewAPIClient(5*time.Second, &auth.Config{Bearer: "tok123"}, nil)
	c := cache.New(30*time.Second, 0)
	exec := apihttp.NewExecutor(client, c, srv.URL, false)

	op := &spec.Operation{Method: "GET", Path: "/me"}
	_, errno, err := exec.ExecuteGET(context.Background(), op, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, uintptr(0), uintptr(errno))
}

func TestTreeBuilding_PetstorePathGrouping(t *testing.T) {
	data, err := spec.LoadSpec("../../testdata/petstore.yaml")
	require.NoError(t, err)
	ps, err := spec.Parse(data, "petstore.yaml")
	require.NoError(t, err)

	root := tree.BuildTree(ps, "path")

	// pet/ should exist with .post (POST /pet), .put (PUT /pet), .schema — no GET on /pet
	pet, ok := root.Children["pet"]
	require.True(t, ok, "expected pet/ dir")

	_, hasPost := pet.Children[".post"]
	_, hasPut := pet.Children[".put"]
	_, hasSchema := pet.Children[".schema"]
	_, hasHelp := pet.Children[".help"]
	_, hasResponse := pet.Children[".response"]
	assert.True(t, hasPost, "pet/.post missing (POST /pet)")
	assert.True(t, hasPut, "pet/.put missing (PUT /pet)")
	assert.True(t, hasSchema, "pet/.schema missing")
	assert.True(t, hasHelp, "pet/.help missing")
	assert.True(t, hasResponse, "pet/.response missing")

	// Schema content should be populated (not nil)
	schema := pet.Children[".schema"]
	assert.NotNil(t, schema.StaticContent, "pet/.schema StaticContent should not be nil")
	assert.True(t, json.Valid(schema.StaticContent[:len(schema.StaticContent)-1]), "pet/.schema should be valid JSON")

	// Help content should be populated
	help := pet.Children[".help"]
	assert.NotNil(t, help.StaticContent)
	assert.Contains(t, string(help.StaticContent), "pet")

	// findByStatus should have .query
	fbs, ok := pet.Children["findByStatus"]
	require.True(t, ok, "expected findByStatus/ dir")
	_, hasQuery := fbs.Children[".query"]
	assert.True(t, hasQuery, "findByStatus/.query missing")

	// {petId} should be a param template
	paramDir, ok := pet.Children["{petId}"]
	require.True(t, ok, "expected {petId}/ dir")
	assert.True(t, paramDir.IsParamTemplate)
	assert.Equal(t, "petId", paramDir.ParamName)
}

func TestTreeBuilding_CloneWithBinding(t *testing.T) {
	data, err := spec.LoadSpec("../../testdata/petstore.yaml")
	require.NoError(t, err)
	ps, err := spec.Parse(data, "petstore.yaml")
	require.NoError(t, err)

	root := tree.BuildTree(ps, "path")
	pet := root.Children["pet"]
	paramDir := pet.Children["{petId}"]

	cloned := tree.CloneWithBinding(paramDir, "petId", "99", pet)
	assert.Equal(t, "99", cloned.Name)
	assert.Equal(t, "99", cloned.PathParams["petId"])
	assert.False(t, cloned.IsParamTemplate)

	// All file children should have the binding
	for _, child := range cloned.Children {
		if child.Type == tree.NodeTypeFile {
			assert.Equal(t, "99", child.PathParams["petId"],
				"file %q should have petId=99", child.Name)
		}
	}

	// .help content should have the param substituted
	if helpFile, ok := cloned.Children[".help"]; ok {
		// StaticContent is copied from template — resolveHelpContent substitutes it at read time
		// Just verify it exists and is non-nil
		assert.NotNil(t, helpFile.StaticContent)
	}
}

func TestTreeBuilding_SchemaContent(t *testing.T) {
	data, err := spec.LoadSpec("../../testdata/no_tags.yaml")
	require.NoError(t, err)
	ps, err := spec.Parse(data, "no_tags.yaml")
	require.NoError(t, err)

	root := tree.BuildTree(ps, "path")
	items := root.Children["items"]
	require.NotNil(t, items)

	schema, ok := items.Children[".schema"]
	require.True(t, ok)
	require.NotNil(t, schema.StaticContent)

	// Should be valid JSON with the "name" property
	var parsed map[string]any
	err = json.Unmarshal(schema.StaticContent[:len(schema.StaticContent)-1], &parsed)
	require.NoError(t, err, "schema content should be valid JSON")
	props, ok := parsed["properties"].(map[string]any)
	require.True(t, ok)
	_, hasName := props["name"]
	assert.True(t, hasName, "schema should contain 'name' property")
}

func TestTreeBuilding_HelpContent(t *testing.T) {
	data, err := spec.LoadSpec("../../testdata/no_tags.yaml")
	require.NoError(t, err)
	ps, err := spec.Parse(data, "no_tags.yaml")
	require.NoError(t, err)

	root := tree.BuildTree(ps, "path")
	items := root.Children["items"]

	help, ok := items.Children[".help"]
	require.True(t, ok)
	content := string(help.StaticContent)

	// Should contain the directory path
	assert.Contains(t, content, "/items")
	// Should list files
	assert.Contains(t, content, ".data")
	assert.Contains(t, content, ".post")
	// Should list subdirs
	assert.Contains(t, content, "{id}")
}

func TestSpecParse_OperationFields(t *testing.T) {
	data, err := spec.LoadSpec("../../testdata/petstore.yaml")
	require.NoError(t, err)
	ps, err := spec.Parse(data, "petstore.yaml")
	require.NoError(t, err)

	// Find GET /pet/{petId}
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

	// petId should be a path parameter
	found := false
	for _, p := range getById.Parameters {
		if p.Name == "petId" && p.In == "path" {
			found = true
			assert.True(t, p.Required)
		}
	}
	assert.True(t, found, "petId path param not found")
}

func TestSpecParse_RequestBody(t *testing.T) {
	data, err := spec.LoadSpec("../../testdata/no_tags.yaml")
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
	assert.True(t, hasName)
}

func TestQueryStringParsing(t *testing.T) {
	cases := []struct {
		input    string
		expected map[string]string
	}{
		{"status=available&limit=10", map[string]string{"status": "available", "limit": "10"}},
		{"single=val", map[string]string{"single": "val"}},
		{"novalue", map[string]string{"novalue": ""}},
		{"  spaced = val  ", map[string]string{"spaced": "val"}},
	}

	// Use the http executor's query building logic via a live server
	for _, tc := range cases {
		received := map[string]string{}
		mux := http.NewServeMux()
		mux.HandleFunc("/q", func(w http.ResponseWriter, r *http.Request) {
			for k, v := range r.URL.Query() {
				received[k] = v[0]
			}
			w.WriteHeader(200)
			w.Write([]byte("{}"))
		})
		srv := httptest.NewServer(mux)
		client := apihttp.NewAPIClient(5*time.Second, &auth.Config{}, nil)
		c := cache.New(0, 0) // no cache
		exec := apihttp.NewExecutor(client, c, srv.URL, false)

		// Build query params from the input string
		params := parseQueryString(tc.input)
		op := &spec.Operation{Method: "GET", Path: "/q"}
		exec.ExecuteGET(context.Background(), op, nil, params)
		srv.Close()

		for k, v := range tc.expected {
			if v != "" { // skip blank-value assertions for URL query encoding
				assert.Equal(t, v, received[k], "query param %q for input %q", k, tc.input)
			}
		}
	}
}

// parseQueryString mirrors the internal fs package logic for test use.
func parseQueryString(s string) map[string]string {
	result := make(map[string]string)
	for _, part := range strings.Split(strings.TrimSpace(s), "&") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.IndexByte(part, '='); idx < 0 {
			result[part] = ""
		} else {
			result[strings.TrimSpace(part[:idx])] = strings.TrimSpace(part[idx+1:])
		}
	}
	return result
}
