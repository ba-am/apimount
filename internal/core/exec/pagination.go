package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// PaginationStrategy identifies how to paginate a response.
type PaginationStrategy string

const (
	PaginationNone       PaginationStrategy = ""
	PaginationLink       PaginationStrategy = "link"
	PaginationCursor     PaginationStrategy = "cursor"
	PaginationOffsetLimit PaginationStrategy = "offset_limit"
	PaginationPageSize   PaginationStrategy = "page_size"
)

// PaginationConfig controls the pagination middleware behaviour.
type PaginationConfig struct {
	MaxPages      int                // cap on total pages fetched (default 100)
	Strategy      PaginationStrategy // force a strategy; empty = auto-detect
	CursorParam   string             // query param name for cursor (default: auto-detect)
	CursorField   string             // JSON field holding next cursor (default: auto-detect)
	PageParam     string             // query param for page number (default "page")
	SizeParam     string             // query param for page size (default "per_page")
	OffsetParam   string             // query param for offset (default "offset")
	LimitParam    string             // query param for limit (default "limit")
}

func (c PaginationConfig) withDefaults() PaginationConfig {
	if c.MaxPages <= 0 {
		c.MaxPages = 100
	}
	if c.PageParam == "" {
		c.PageParam = "page"
	}
	if c.SizeParam == "" {
		c.SizeParam = "per_page"
	}
	if c.OffsetParam == "" {
		c.OffsetParam = "offset"
	}
	if c.LimitParam == "" {
		c.LimitParam = "limit"
	}
	return c
}

// PaginationMiddleware fetches all pages of a paginated GET response and merges
// them into a single JSON array. Non-GET requests and non-paginated responses
// pass through unchanged.
func PaginationMiddleware(cfg PaginationConfig) Middleware {
	cfg = cfg.withDefaults()

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request) (*Result, error) {
			if req.Op == nil || strings.ToUpper(req.Op.Method) != "GET" {
				return next(ctx, req)
			}

			first, err := next(ctx, req)
			if err != nil || first == nil {
				return first, err
			}

			strategy := cfg.Strategy
			if strategy == PaginationNone {
				strategy = detectStrategy(first, req)
			}
			if strategy == PaginationNone {
				return first, nil
			}

			return paginate(ctx, next, req, first, strategy, cfg)
		}
	}
}

func detectStrategy(result *Result, _ *Request) PaginationStrategy {
	if linkNext(result.Headers) != "" {
		return PaginationLink
	}

	if result.Body != nil {
		var obj map[string]json.RawMessage
		if json.Unmarshal(result.Body, &obj) == nil {
			for _, key := range []string{"next_cursor", "cursor", "after", "continuation_token", "nextToken"} {
				if raw, ok := obj[key]; ok {
					var val string
					if json.Unmarshal(raw, &val) == nil && val != "" {
						return PaginationCursor
					}
				}
			}
		}
	}

	return PaginationNone
}

func paginate(ctx context.Context, next Handler, req *Request, first *Result, strategy PaginationStrategy, cfg PaginationConfig) (*Result, error) {
	allItems := extractItems(first.Body)
	pages := 1

	switch strategy {
	case PaginationLink:
		return paginateLink(ctx, next, req, first, allItems, pages, cfg)
	case PaginationCursor:
		return paginateCursor(ctx, next, req, first, allItems, pages, cfg)
	case PaginationOffsetLimit:
		return paginateOffsetLimit(ctx, next, req, first, allItems, pages, cfg)
	case PaginationPageSize:
		return paginatePageSize(ctx, next, req, first, allItems, pages, cfg)
	default:
		return first, nil
	}
}

func paginateLink(ctx context.Context, next Handler, req *Request, current *Result, allItems []json.RawMessage, pages int, cfg PaginationConfig) (*Result, error) {
	for pages < cfg.MaxPages {
		nextURL := linkNext(current.Headers)
		if nextURL == "" {
			break
		}

		pageReq := cloneRequestWithURL(req, nextURL)
		result, err := next(ctx, pageReq)
		if err != nil {
			break
		}

		items := extractItems(result.Body)
		if len(items) == 0 {
			break
		}
		allItems = append(allItems, items...)
		current = result
		pages++
	}

	return mergedResult(current, allItems, pages), nil
}

func paginateCursor(ctx context.Context, next Handler, req *Request, current *Result, allItems []json.RawMessage, pages int, cfg PaginationConfig) (*Result, error) {
	cursorParam := cfg.CursorParam
	cursorField := cfg.CursorField

	if cursorField == "" {
		cursorField = detectCursorField(current.Body)
	}
	if cursorParam == "" {
		cursorParam = detectCursorParam(cursorField)
	}
	if cursorField == "" {
		return current, nil
	}

	for pages < cfg.MaxPages {
		cursor := extractStringField(current.Body, cursorField)
		if cursor == "" {
			break
		}

		pageReq := cloneRequestWithQuery(req, cursorParam, cursor)
		result, err := next(ctx, pageReq)
		if err != nil {
			break
		}

		items := extractItems(result.Body)
		if len(items) == 0 {
			break
		}
		allItems = append(allItems, items...)
		current = result
		pages++
	}

	return mergedResult(current, allItems, pages), nil
}

func paginateOffsetLimit(ctx context.Context, next Handler, req *Request, current *Result, allItems []json.RawMessage, pages int, cfg PaginationConfig) (*Result, error) {
	limit := 0
	if req.QueryParams != nil {
		if v, err := strconv.Atoi(req.QueryParams[cfg.LimitParam]); err == nil {
			limit = v
		}
	}
	if limit == 0 {
		limit = len(allItems)
	}
	if limit == 0 {
		return current, nil
	}

	offset := len(allItems)
	for pages < cfg.MaxPages {
		pageReq := cloneRequestWithQuery(req, cfg.OffsetParam, strconv.Itoa(offset))
		pageReq = cloneRequestWithQuery(pageReq, cfg.LimitParam, strconv.Itoa(limit))
		result, err := next(ctx, pageReq)
		if err != nil {
			break
		}

		items := extractItems(result.Body)
		if len(items) == 0 {
			break
		}
		allItems = append(allItems, items...)
		current = result
		offset += len(items)
		pages++

		if len(items) < limit {
			break
		}
	}

	return mergedResult(current, allItems, pages), nil
}

func paginatePageSize(ctx context.Context, next Handler, req *Request, current *Result, allItems []json.RawMessage, pages int, cfg PaginationConfig) (*Result, error) {
	page := 2
	if req.QueryParams != nil {
		if v, err := strconv.Atoi(req.QueryParams[cfg.PageParam]); err == nil {
			page = v + 1
		}
	}

	for pages < cfg.MaxPages {
		pageReq := cloneRequestWithQuery(req, cfg.PageParam, strconv.Itoa(page))
		result, err := next(ctx, pageReq)
		if err != nil {
			break
		}

		items := extractItems(result.Body)
		if len(items) == 0 {
			break
		}
		allItems = append(allItems, items...)
		current = result
		page++
		pages++
	}

	return mergedResult(current, allItems, pages), nil
}

// extractItems tries to pull an array from the response body. If the body is a
// JSON array, returns its elements. If the body is an object with a single
// array-valued field (or a known key like "items", "data", "results"), returns
// that array's elements.
func extractItems(body []byte) []json.RawMessage {
	if body == nil {
		return nil
	}
	var arr []json.RawMessage
	if json.Unmarshal(body, &arr) == nil {
		return arr
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return nil
	}
	for _, key := range []string{"items", "data", "results", "records", "entries", "values"} {
		if raw, ok := obj[key]; ok {
			if json.Unmarshal(raw, &arr) == nil {
				return arr
			}
		}
	}
	for _, raw := range obj {
		if json.Unmarshal(raw, &arr) == nil {
			return arr
		}
	}
	return nil
}

var linkNextRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func linkNext(headers map[string]string) string {
	if headers == nil {
		return ""
	}
	link, ok := headers["Link"]
	if !ok {
		return ""
	}
	m := linkNextRe.FindStringSubmatch(link)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func detectCursorField(body []byte) string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return ""
	}
	for _, key := range []string{"next_cursor", "cursor", "after", "continuation_token", "nextToken", "next_page_token"} {
		if _, ok := obj[key]; ok {
			return key
		}
	}
	return ""
}

func detectCursorParam(field string) string {
	switch field {
	case "next_cursor", "cursor":
		return "cursor"
	case "after":
		return "after"
	case "continuation_token":
		return "continuation_token"
	case "nextToken", "next_page_token":
		return "pageToken"
	default:
		return "cursor"
	}
}

func extractStringField(body []byte, field string) string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return ""
	}
	raw, ok := obj[field]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

func cloneRequestWithURL(req *Request, rawURL string) *Request {
	clone := *req
	u, err := url.Parse(rawURL)
	if err != nil {
		return &clone
	}
	newOp := *req.Op
	newOp.Path = u.Path
	clone.Op = &newOp
	clone.QueryParams = make(map[string]string)
	for k, v := range req.QueryParams {
		clone.QueryParams[k] = v
	}
	for k, vs := range u.Query() {
		if len(vs) > 0 {
			clone.QueryParams[k] = vs[0]
		}
	}
	return &clone
}

func cloneRequestWithQuery(req *Request, key, value string) *Request {
	clone := *req
	clone.QueryParams = make(map[string]string)
	for k, v := range req.QueryParams {
		clone.QueryParams[k] = v
	}
	clone.QueryParams[key] = value
	return &clone
}

func mergedResult(last *Result, items []json.RawMessage, pages int) *Result {
	body, _ := json.Marshal(items)
	return &Result{
		Status:     last.Status,
		Headers:    mergeHeaders(last.Headers, pages),
		Body:       body,
		DurationMs: last.DurationMs,
		Attempts:   last.Attempts,
	}
}

func mergeHeaders(headers map[string]string, pages int) map[string]string {
	out := make(map[string]string)
	for k, v := range headers {
		out[k] = v
	}
	out["X-Apimount-Pages"] = fmt.Sprintf("%d", pages)
	return out
}
