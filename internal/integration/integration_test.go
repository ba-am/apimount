// Package integration exercises the full stack (spec → plan → exec) without a live FUSE mount.
package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimount/apimount/internal/core/auth"
	"github.com/apimount/apimount/internal/core/cache"
	"github.com/apimount/apimount/internal/core/exec"
	"github.com/apimount/apimount/internal/core/plan"
	"github.com/apimount/apimount/internal/core/spec"
)

func mockAPI(t *testing.T, mux *http.ServeMux) (*httptest.Server, *exec.Executor) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := exec.NewAPIClient(5*time.Second, &auth.Config{}, nil)
	c := cache.New(30*time.Second, 0)
	ex := exec.NewExecutor(client, c, srv.URL, true)
	return srv, ex
}

func TestFullFlow_GETList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/pets", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "name": "Fido", "status": "available"}})
	})
	_, ex := mockAPI(t, mux)

	op := &spec.Operation{Method: "GET", Path: "/pets"}
	body, errno, err := ex.ExecuteGET(context.Background(), op, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, uintptr(0), uintptr(errno))
	assert.Contains(t, string(body), "Fido")
}

func TestFullFlow_GETByPathParam(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/pets/42", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "Rex"})
	})
	_, ex := mockAPI(t, mux)

	op := &spec.Operation{Method: "GET", Path: "/pets/{petId}"}
	body, errno, err := ex.ExecuteGET(context.Background(), op, map[string]string{"petId": "42"}, nil)
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
	_, ex := mockAPI(t, mux)

	op := &spec.Operation{Method: "GET", Path: "/pets"}
	body, _, err := ex.ExecuteGET(context.Background(), op, nil, map[string]string{"status": "available"})
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
	_, ex := mockAPI(t, mux)

	op := &spec.Operation{Method: "POST", Path: "/pets"}
	payload := []byte(`{"name":"New","photoUrls":[]}`)
	body, errno, err := ex.ExecuteWrite(context.Background(), op, nil, nil, payload)
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
	_, ex := mockAPI(t, mux)

	op := &spec.Operation{Method: "PUT", Path: "/pets/{petId}"}
	body, errno, err := ex.ExecuteWrite(context.Background(), op, map[string]string{"petId": "5"}, nil, []byte(`{"name":"Updated"}`))
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
	_, ex := mockAPI(t, mux)

	op := &spec.Operation{Method: "DELETE", Path: "/pets/{petId}"}
	_, _, err := ex.ExecuteWrite(context.Background(), op, map[string]string{"petId": "7"}, nil, nil)
	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestFullFlow_GET_401(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/secret", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	_, ex := mockAPI(t, mux)

	op := &spec.Operation{Method: "GET", Path: "/secret"}
	body, errno, _ := ex.ExecuteGET(context.Background(), op, nil, nil)
	assert.NotEqual(t, uintptr(0), uintptr(errno))
	assert.Contains(t, string(body), "401")
}

func TestFullFlow_GET_404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/pets/999", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, ex := mockAPI(t, mux)

	op := &spec.Operation{Method: "GET", Path: "/pets/{petId}"}
	body, errno, _ := ex.ExecuteGET(context.Background(), op, map[string]string{"petId": "999"}, nil)
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
	_, ex := mockAPI(t, mux)

	op := &spec.Operation{Method: "GET", Path: "/pets"}
	ex.ExecuteGET(context.Background(), op, nil, nil)
	ex.ExecuteGET(context.Background(), op, nil, nil)
	ex.ExecuteGET(context.Background(), op, nil, nil)
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
	_, ex := mockAPI(t, mux)
	op := &spec.Operation{Method: "GET", Path: "/pets"}
	postOp := &spec.Operation{Method: "POST", Path: "/pets"}

	ex.ExecuteGET(context.Background(), op, nil, nil)
	ex.ExecuteGET(context.Background(), op, nil, nil)
	ex.ExecuteWrite(context.Background(), postOp, nil, nil, []byte(`{}`))
	ex.ExecuteGET(context.Background(), op, nil, nil)
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
	client := exec.NewAPIClient(5*time.Second, &auth.Config{Bearer: "tok123"}, nil)
	c := cache.New(30*time.Second, 0)
	ex := exec.NewExecutor(client, c, srv.URL, false)

	op := &spec.Operation{Method: "GET", Path: "/me"}
	_, errno, err := ex.ExecuteGET(context.Background(), op, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, uintptr(0), uintptr(errno))
}

func TestTreeBuilding_PetstorePathGrouping(t *testing.T) {
	data, err := spec.LoadSpec("../../testdata/petstore.yaml")
	require.NoError(t, err)
	ps, err := spec.Parse(data, "petstore.yaml")
	require.NoError(t, err)

	root := plan.BuildTree(ps, "path")

	pet, ok := root.Children["pet"]
	require.True(t, ok, "expected pet/ dir")

	_, hasPost := pet.Children[".post"]
	_, hasPut := pet.Children[".put"]
	_, hasSchema := pet.Children[".schema"]
	_, hasHelp := pet.Children[".help"]
	_, hasResponse := pet.Children[".response"]
	assert.True(t, hasPost, "pet/.post missing")
	assert.True(t, hasPut, "pet/.put missing")
	assert.True(t, hasSchema, "pet/.schema missing")
	assert.True(t, hasHelp, "pet/.help missing")
	assert.True(t, hasResponse, "pet/.response missing")

	schema := pet.Children[".schema"]
	assert.NotNil(t, schema.StaticContent, "pet/.schema StaticContent should not be nil")
	assert.True(t, json.Valid(schema.StaticContent[:len(schema.StaticContent)-1]))

	fbs, ok := pet.Children["findByStatus"]
	require.True(t, ok, "expected findByStatus/ dir")
	_, hasQuery := fbs.Children[".query"]
	assert.True(t, hasQuery, "findByStatus/.query missing")

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

	root := plan.BuildTree(ps, "path")
	pet := root.Children["pet"]
	paramDir := pet.Children["{petId}"]

	cloned := plan.CloneWithBinding(paramDir, "petId", "99", pet)
	assert.Equal(t, "99", cloned.Name)
	assert.Equal(t, "99", cloned.PathParams["petId"])
	assert.False(t, cloned.IsParamTemplate)

	for _, child := range cloned.Children {
		if child.Type == plan.NodeTypeFile {
			assert.Equal(t, "99", child.PathParams["petId"], "file %q should have petId=99", child.Name)
		}
	}
}

func TestTreeBuilding_SchemaContent(t *testing.T) {
	data, err := spec.LoadSpec("../../testdata/no_tags.yaml")
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

	root := plan.BuildTree(ps, "path")
	items := root.Children["items"]

	help, ok := items.Children[".help"]
	require.True(t, ok)
	content := string(help.StaticContent)

	assert.Contains(t, content, "/items")
	assert.Contains(t, content, ".data")
	assert.Contains(t, content, ".post")
	assert.Contains(t, content, "{id}")
}

func TestSpecParse_OperationFields(t *testing.T) {
	data, err := spec.LoadSpec("../../testdata/petstore.yaml")
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

func TestQueryStringBuilding(t *testing.T) {
	cases := []struct {
		params   map[string]string
		expected map[string]string
	}{
		{map[string]string{"status": "available", "limit": "10"}, map[string]string{"status": "available", "limit": "10"}},
		{map[string]string{"single": "val"}, map[string]string{"single": "val"}},
	}

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
		client := exec.NewAPIClient(5*time.Second, &auth.Config{}, nil)
		c := cache.New(0, 0)
		ex := exec.NewExecutor(client, c, srv.URL, false)

		op := &spec.Operation{Method: "GET", Path: "/q"}
		ex.ExecuteGET(context.Background(), op, nil, tc.params)
		srv.Close()

		for k, v := range tc.expected {
			assert.Equal(t, v, received[k], "query param %q", k)
		}
	}
}
