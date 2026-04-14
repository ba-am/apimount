package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/apimount/apimount/internal/core/cache"
	"github.com/apimount/apimount/internal/core/spec"
)

// Executor orchestrates the full pipeline: cache lookup → auth → HTTP → cache store.
// Additional middlewares (retry, ratelimit, pagination) are added in later phases.
type Executor struct {
	client     *APIClient
	cache      *cache.Cache
	baseURL    string
	prettyJSON bool
	handler    Handler // assembled middleware chain
}

// NewExecutor creates a new Executor.
func NewExecutor(client *APIClient, c *cache.Cache, baseURL string, prettyJSON bool) *Executor {
	e := &Executor{
		client:     client,
		cache:      c,
		baseURL:    strings.TrimRight(baseURL, "/"),
		prettyJSON: prettyJSON,
	}
	e.handler = chain(e.transport, nil) // Phase 4 will add retry/ratelimit here
	return e
}

// ExecuteGET executes an HTTP GET for the given operation and path params.
// Returns (body, errno, error) — errno is 0 on success.
func (e *Executor) ExecuteGET(ctx context.Context, op *spec.Operation, pathParams map[string]string, queryParams map[string]string) ([]byte, syscall.Errno, error) {
	resolvedURL, err := e.resolveURL(op.Path, pathParams)
	if err != nil {
		return nil, syscall.EIO, err
	}

	cacheKey := cache.Key("GET", resolvedURL, queryParams)
	if cached, ok := e.cache.Get(cacheKey); ok {
		return cached, 0, nil
	}

	resp, err := e.client.Execute(ctx, &HTTPRequest{
		Method:      "GET",
		URL:         resolvedURL,
		QueryParams: queryParams,
		OpSecurity:  op.Security,
	})
	if err != nil {
		return nil, httpErrToErrno(err), err
	}

	body, errno := e.processResponse(resp)
	if errno != 0 {
		return body, errno, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(resp.Body))
	}

	e.cache.Set(cacheKey, body)
	return body, 0, nil
}

// ExecuteWrite executes a write operation (POST/PUT/PATCH/DELETE).
// Returns (responseBody, errno, error).
func (e *Executor) ExecuteWrite(ctx context.Context, op *spec.Operation, pathParams map[string]string, queryParams map[string]string, body []byte) ([]byte, syscall.Errno, error) {
	resolvedURL, err := e.resolveURL(op.Path, pathParams)
	if err != nil {
		return nil, syscall.EIO, err
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
		return nil, httpErrToErrno(err), err
	}

	e.cache.Invalidate(resolvedURL)
	if parent := parentPath(resolvedURL); parent != "" {
		e.cache.Invalidate(parent)
	}

	responseBody, errno := e.processResponse(resp)
	if errno != 0 {
		return responseBody, errno, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(resp.Body))
	}
	return responseBody, 0, nil
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
	body, errno := e.processResponse(resp)
	return &Result{
		Status:     resp.StatusCode,
		Headers:    resp.Headers,
		Body:       body,
		Errno:      errno,
		DurationMs: time.Since(start).Milliseconds(),
		Attempts:   1,
	}, nil
}

func (e *Executor) resolveURL(path string, pathParams map[string]string) (string, error) {
	resolved := path
	for k, v := range pathParams {
		resolved = strings.ReplaceAll(resolved, "{"+k+"}", url.PathEscape(v))
	}
	return e.baseURL + resolved, nil
}

func (e *Executor) processResponse(resp *HTTPResponse) ([]byte, syscall.Errno) {
	if errno := statusToErrno(resp.StatusCode); errno != 0 {
		return []byte(errorMessage(resp)), errno
	}
	body := resp.Body
	if e.prettyJSON && isJSON(resp) {
		if pretty, err := prettyPrint(body); err == nil {
			body = pretty
		}
	}
	return body, 0
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

func errorMessage(resp *HTTPResponse) string {
	switch resp.StatusCode {
	case 401:
		return "401 Unauthorized: check auth flags\n"
	case 403:
		return "403 Forbidden: insufficient permissions\n"
	case 404:
		return "404 Not Found\n"
	case 429:
		if ra := resp.Headers["Retry-After"]; ra != "" {
			return fmt.Sprintf("429 Rate Limited: retry after %ss\n", ra)
		}
		return "429 Rate Limited\n"
	default:
		if len(resp.Body) > 0 {
			return string(resp.Body) + "\n"
		}
		return fmt.Sprintf("HTTP %d\n", resp.StatusCode)
	}
}

func statusToErrno(code int) syscall.Errno {
	switch {
	case code >= 200 && code < 300:
		return 0
	case code == 400:
		return syscall.EIO
	case code == 401:
		return syscall.EACCES
	case code == 403:
		return syscall.EPERM
	case code == 404:
		return syscall.ENOENT
	case code == 429:
		return syscall.EAGAIN
	case code >= 500:
		return syscall.EIO
	default:
		return syscall.EIO
	}
}

func httpErrToErrno(err error) syscall.Errno {
	if err == nil {
		return 0
	}
	s := err.Error()
	if strings.Contains(s, "connection refused") {
		return syscall.ECONNREFUSED
	}
	if strings.Contains(s, "timeout") || strings.Contains(s, "deadline exceeded") {
		return syscall.ETIMEDOUT
	}
	return syscall.EIO
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
