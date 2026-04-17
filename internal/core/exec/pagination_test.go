package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/apimount/apimount/internal/core/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaginationMiddleware_NonGETPassesThrough(t *testing.T) {
	handler := PaginationMiddleware(PaginationConfig{})(
		func(_ context.Context, _ *Request) (*Result, error) {
			return &Result{Status: 201, Body: []byte(`{"id":1}`)}, nil
		},
	)
	res, err := handler(context.Background(), &Request{Op: &spec.Operation{Method: "POST"}})
	require.NoError(t, err)
	assert.Equal(t, 201, res.Status)
}

func TestPaginationMiddleware_NoPagination(t *testing.T) {
	handler := PaginationMiddleware(PaginationConfig{})(
		func(_ context.Context, _ *Request) (*Result, error) {
			return &Result{Status: 200, Body: []byte(`[{"id":1}]`)}, nil
		},
	)
	res, err := handler(context.Background(), &Request{Op: &spec.Operation{Method: "GET"}})
	require.NoError(t, err)
	assert.Equal(t, 200, res.Status)
	assert.Equal(t, `[{"id":1}]`, string(res.Body))
}

func TestPaginationMiddleware_LinkStrategy(t *testing.T) {
	page := 0
	handler := PaginationMiddleware(PaginationConfig{MaxPages: 5})(
		func(_ context.Context, req *Request) (*Result, error) {
			page++
			headers := map[string]string{}
			if page < 3 {
				headers["Link"] = fmt.Sprintf(`<http://api.example.com/items?page=%d>; rel="next"`, page+1)
			}
			return &Result{
				Status:  200,
				Headers: headers,
				Body:    []byte(fmt.Sprintf(`[{"id":%d}]`, page)),
			}, nil
		},
	)

	res, err := handler(context.Background(), &Request{
		Op: &spec.Operation{Method: "GET", Path: "/items"},
	})
	require.NoError(t, err)

	var items []json.RawMessage
	require.NoError(t, json.Unmarshal(res.Body, &items))
	assert.Len(t, items, 3)
	assert.Equal(t, "3", res.Headers["X-Apimount-Pages"])
}

func TestPaginationMiddleware_CursorStrategy(t *testing.T) {
	cursors := []string{"abc", "def", ""}
	page := 0
	handler := PaginationMiddleware(PaginationConfig{MaxPages: 10})(
		func(_ context.Context, req *Request) (*Result, error) {
			idx := page
			page++
			body := fmt.Sprintf(`{"data":[{"id":%d}],"cursor":"%s"}`, idx+1, cursors[idx])
			return &Result{Status: 200, Body: []byte(body)}, nil
		},
	)

	res, err := handler(context.Background(), &Request{
		Op:          &spec.Operation{Method: "GET", Path: "/items"},
		QueryParams: map[string]string{},
	})
	require.NoError(t, err)

	var items []json.RawMessage
	require.NoError(t, json.Unmarshal(res.Body, &items))
	assert.Len(t, items, 3)
	assert.Equal(t, "3", res.Headers["X-Apimount-Pages"])
}

func TestPaginationMiddleware_PageSizeStrategy(t *testing.T) {
	page := 0
	handler := PaginationMiddleware(PaginationConfig{
		MaxPages: 10,
		Strategy: PaginationPageSize,
	})(
		func(_ context.Context, req *Request) (*Result, error) {
			page++
			if page > 3 {
				return &Result{Status: 200, Body: []byte(`[]`)}, nil
			}
			return &Result{
				Status: 200,
				Body:   []byte(fmt.Sprintf(`[{"id":%d}]`, page)),
			}, nil
		},
	)

	res, err := handler(context.Background(), &Request{
		Op:          &spec.Operation{Method: "GET", Path: "/items"},
		QueryParams: map[string]string{},
	})
	require.NoError(t, err)

	var items []json.RawMessage
	require.NoError(t, json.Unmarshal(res.Body, &items))
	assert.Len(t, items, 3)
}

func TestPaginationMiddleware_OffsetLimitStrategy(t *testing.T) {
	handler := PaginationMiddleware(PaginationConfig{
		MaxPages: 10,
		Strategy: PaginationOffsetLimit,
	})(
		func(_ context.Context, req *Request) (*Result, error) {
			offset := req.QueryParams["offset"]
			switch offset {
			case "", "0":
				return &Result{Status: 200, Body: []byte(`[{"id":1},{"id":2}]`)}, nil
			case "2":
				return &Result{Status: 200, Body: []byte(`[{"id":3}]`)}, nil
			default:
				return &Result{Status: 200, Body: []byte(`[]`)}, nil
			}
		},
	)

	res, err := handler(context.Background(), &Request{
		Op:          &spec.Operation{Method: "GET", Path: "/items"},
		QueryParams: map[string]string{"limit": "2"},
	})
	require.NoError(t, err)

	var items []json.RawMessage
	require.NoError(t, json.Unmarshal(res.Body, &items))
	assert.Len(t, items, 3)
}

func TestPaginationMiddleware_MaxPagesRespected(t *testing.T) {
	page := 0
	handler := PaginationMiddleware(PaginationConfig{MaxPages: 2})(
		func(_ context.Context, req *Request) (*Result, error) {
			page++
			headers := map[string]string{
				"Link": `<http://api.example.com/items?page=next>; rel="next"`,
			}
			return &Result{
				Status:  200,
				Headers: headers,
				Body:    []byte(fmt.Sprintf(`[{"id":%d}]`, page)),
			}, nil
		},
	)

	res, err := handler(context.Background(), &Request{
		Op: &spec.Operation{Method: "GET", Path: "/items"},
	})
	require.NoError(t, err)

	var items []json.RawMessage
	require.NoError(t, json.Unmarshal(res.Body, &items))
	assert.Len(t, items, 2)
	assert.Equal(t, "2", res.Headers["X-Apimount-Pages"])
}

func TestExtractItems_Array(t *testing.T) {
	items := extractItems([]byte(`[1,2,3]`))
	assert.Len(t, items, 3)
}

func TestExtractItems_ObjectWithDataField(t *testing.T) {
	items := extractItems([]byte(`{"data":[1,2],"total":2}`))
	assert.Len(t, items, 2)
}

func TestExtractItems_ObjectWithItemsField(t *testing.T) {
	items := extractItems([]byte(`{"items":[1],"next":null}`))
	assert.Len(t, items, 1)
}

func TestExtractItems_NilBody(t *testing.T) {
	items := extractItems(nil)
	assert.Nil(t, items)
}

func TestLinkNext(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"nil", nil, ""},
		{"no link", map[string]string{}, ""},
		{"next present", map[string]string{"Link": `<http://example.com/page2>; rel="next"`}, "http://example.com/page2"},
		{"only prev", map[string]string{"Link": `<http://example.com/page1>; rel="prev"`}, ""},
		{"multiple", map[string]string{"Link": `<http://example.com/p1>; rel="prev", <http://example.com/p3>; rel="next"`}, "http://example.com/p3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, linkNext(tt.headers))
		})
	}
}

func TestPaginationConfig_Defaults(t *testing.T) {
	cfg := PaginationConfig{}.withDefaults()
	assert.Equal(t, 100, cfg.MaxPages)
	assert.Equal(t, "page", cfg.PageParam)
	assert.Equal(t, "per_page", cfg.SizeParam)
	assert.Equal(t, "offset", cfg.OffsetParam)
	assert.Equal(t, "limit", cfg.LimitParam)
}
