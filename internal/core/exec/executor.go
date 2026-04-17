package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/apimount/apimount/internal/core/cache"
	"github.com/apimount/apimount/internal/core/spec"
)

// MiddlewareConfig bundles all Phase 4 middleware settings.
type MiddlewareConfig struct {
	Retry      RetryConfig
	RateLimit  RateLimitConfig
	Pagination PaginationConfig
	Validation ValidationConfig
}

// Executor orchestrates the full pipeline: cache lookup → auth → HTTP → cache store.
type Executor struct {
	client     *APIClient
	cache      *cache.Cache
	baseURL    string
	prettyJSON bool
	handler    Handler // assembled middleware chain
}

// NewExecutor creates a new Executor with default (no-op) middleware settings.
func NewExecutor(client *APIClient, c *cache.Cache, baseURL string, prettyJSON bool) *Executor {
	return NewExecutorWithMiddleware(client, c, baseURL, prettyJSON, MiddlewareConfig{})
}

// NewExecutorWithMiddleware creates an Executor with the given middleware config.
// Middleware order (outermost first): validation → pagination → retry → ratelimit → transport.
func NewExecutorWithMiddleware(client *APIClient, c *cache.Cache, baseURL string, prettyJSON bool, mwCfg MiddlewareConfig) *Executor {
	e := &Executor{
		client:     client,
		cache:      c,
		baseURL:    strings.TrimRight(baseURL, "/"),
		prettyJSON: prettyJSON,
	}
	var middlewares []Middleware
	if mwCfg.Validation.Enabled {
		middlewares = append(middlewares, ValidateMiddleware(mwCfg.Validation))
	}
	middlewares = append(middlewares, PaginationMiddleware(mwCfg.Pagination))
	middlewares = append(middlewares, RetryMiddleware(mwCfg.Retry))
	middlewares = append(middlewares, RateLimitMiddleware(mwCfg.RateLimit))
	e.handler = chain(e.transport, middlewares)
	return e
}

// HTTPError represents a non-2xx upstream response.
type HTTPError struct {
	Status int
	Body   []byte
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, string(e.Body))
}

// ExecuteGET executes an HTTP GET for the given operation and path params.
func (e *Executor) ExecuteGET(ctx context.Context, op *spec.Operation, pathParams map[string]string, queryParams map[string]string) ([]byte, error) {
	resolvedURL, err := e.resolveURL(op.Path, pathParams)
	if err != nil {
		return nil, err
	}

	cacheKey := cache.Key("GET", resolvedURL, queryParams)
	if cached, ok := e.cache.Get(cacheKey); ok {
		return cached, nil
	}

	resp, err := e.client.Execute(ctx, &HTTPRequest{
		Method:      "GET",
		URL:         resolvedURL,
		QueryParams: queryParams,
		OpSecurity:  op.Security,
	})
	if err != nil {
		return nil, err
	}

	body, httpErr := e.processResponse(resp)
	if httpErr != nil {
		return body, httpErr
	}

	e.cache.Set(cacheKey, body)
	return body, nil
}

// ExecuteWrite executes a write operation (POST/PUT/PATCH/DELETE).
func (e *Executor) ExecuteWrite(ctx context.Context, op *spec.Operation, pathParams map[string]string, queryParams map[string]string, body []byte) ([]byte, error) {
	resolvedURL, err := e.resolveURL(op.Path, pathParams)
	if err != nil {
		return nil, err
	}

	ct := ""
	if op.RequestBody != nil {
		ct = op.RequestBody.ContentType
	}

	resp, err := e.client.Execute(ctx, &HTTPRequest{
		Method:      op.Method,
		URL:         resolvedURL,
		Body:        body,
		ContentType: ct,
		OpSecurity:  op.Security,
	})
	if err != nil {
		return nil, err
	}

	e.cache.Invalidate(resolvedURL)
	if parent := parentPath(resolvedURL); parent != "" {
		e.cache.Invalidate(parent)
	}

	responseBody, httpErr := e.processResponse(resp)
	if httpErr != nil {
		return responseBody, httpErr
	}
	return responseBody, nil
}

// transport is the terminal handler: it calls the underlying APIClient.
func (e *Executor) transport(ctx context.Context, req *Request) (*Result, error) {
	start := time.Now()
	resp, err := e.client.Execute(ctx, &HTTPRequest{
		Method:      req.Op.Method,
		URL:         e.baseURL + req.Op.Path,
		QueryParams: req.QueryParams,
		Body:        req.Body,
		OpSecurity:  req.Op.Security,
	})
	if err != nil {
		return nil, err
	}
	body, httpErr := e.processResponse(resp)
	result := &Result{
		Status:     resp.StatusCode,
		Headers:    resp.Headers,
		Body:       body,
		DurationMs: time.Since(start).Milliseconds(),
		Attempts:   1,
	}
	if httpErr != nil {
		return result, httpErr
	}
	return result, nil
}

func (e *Executor) resolveURL(path string, pathParams map[string]string) (string, error) {
	resolved := path
	for k, v := range pathParams {
		resolved = strings.ReplaceAll(resolved, "{"+k+"}", url.PathEscape(v))
	}
	return e.baseURL + resolved, nil
}

func (e *Executor) processResponse(resp *HTTPResponse) ([]byte, error) {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		body := resp.Body
		if e.prettyJSON && isJSON(resp) {
			if pretty, err := prettyPrint(body); err == nil {
				body = pretty
			}
		}
		return body, nil
	}
	return resp.Body, &HTTPError{Status: resp.StatusCode, Body: resp.Body}
}

func isJSON(resp *HTTPResponse) bool {
	return strings.Contains(resp.Headers["Content-Type"], "json")
}

func prettyPrint(data []byte) ([]byte, error) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return json.MarshalIndent(v, "", "  ")
}

func parentPath(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}
	p := u.Path
	idx := strings.LastIndex(strings.TrimRight(p, "/"), "/")
	if idx <= 0 {
		return ""
	}
	u.Path = p[:idx]
	return u.String()
}

// FormatFullResponse returns the full response in HTTP/1.1-style format.
func (e *Executor) FormatFullResponse(resp *HTTPResponse) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "HTTP/1.1 %d\n", resp.StatusCode)
	for k, v := range resp.Headers {
		fmt.Fprintf(&buf, "%s: %s\n", k, v)
	}
	buf.WriteString("\n")
	buf.Write(resp.Body)
	buf.WriteString("\n")
	return buf.Bytes()
}
