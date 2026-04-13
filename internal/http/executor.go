package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"syscall"

	"github.com/apimount/apimount/internal/cache"
	"github.com/apimount/apimount/internal/spec"
)

// Executor executes HTTP operations derived from the filesystem tree.
type Executor struct {
	client     *APIClient
	cache      *cache.Cache
	baseURL    string
	prettyJSON bool
}

// NewExecutor creates a new Executor.
func NewExecutor(client *APIClient, cache *cache.Cache, baseURL string, prettyJSON bool) *Executor {
	return &Executor{
		client:     client,
		cache:      cache,
		baseURL:    strings.TrimRight(baseURL, "/"),
		prettyJSON: prettyJSON,
	}
}

// ExecuteGET executes an HTTP GET for the given operation and path params.
// Returns (body, errno, error).
func (e *Executor) ExecuteGET(ctx context.Context, op *spec.Operation, pathParams map[string]string, queryParams map[string]string) ([]byte, syscall.Errno, error) {
	resolvedURL, err := e.resolveURL(op.Path, pathParams)
	if err != nil {
		return nil, syscall.EIO, err
	}

	cacheKey := cache.Key("GET", resolvedURL, queryParams)
	if cached, ok := e.cache.Get(cacheKey); ok {
		return cached, 0, nil
	}

	resp, err := e.client.Execute(ctx, &Request{
		Method:      "GET",
		URL:         resolvedURL,
		QueryParams: queryParams,
		OpSecurity:  op.Security,
	})
	if err != nil {
		return nil, httpErrToErrno(err), err
	}

	body, errno := e.processResponse(resp, op)
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

	resp, err := e.client.Execute(ctx, &Request{
		Method:      op.Method,
		URL:         resolvedURL,
		Body:        body,
		ContentType: ct,
		OpSecurity:  op.Security,
	})
	if err != nil {
		return nil, httpErrToErrno(err), err
	}

	// Invalidate GET cache for this path and parent
	e.cache.Invalidate(resolvedURL)
	if parent := parentPath(resolvedURL); parent != "" {
		e.cache.Invalidate(parent)
	}

	responseBody, errno := e.processResponse(resp, op)
	if errno != 0 {
		return responseBody, errno, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(resp.Body))
	}
	return responseBody, 0, nil
}

// FormatFullResponse returns the full response in HTTP/1.1-style format.
func (e *Executor) FormatFullResponse(resp *Response) []byte {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("HTTP/1.1 %d\n", resp.StatusCode))
	for k, v := range resp.Headers {
		buf.WriteString(fmt.Sprintf("%s: %s\n", k, v))
	}
	buf.WriteString("\n")
	buf.Write(resp.Body)
	buf.WriteString("\n")
	return buf.Bytes()
}

func (e *Executor) resolveURL(path string, pathParams map[string]string) (string, error) {
	resolved := path
	for k, v := range pathParams {
		resolved = strings.ReplaceAll(resolved, "{"+k+"}", url.PathEscape(v))
	}
	return e.baseURL + resolved, nil
}

func (e *Executor) processResponse(resp *Response, op *spec.Operation) ([]byte, syscall.Errno) {
	if errno := statusToErrno(resp.StatusCode); errno != 0 {
		msg := errorMessage(resp)
		return []byte(msg), errno
	}

	body := resp.Body
	if e.prettyJSON && isJSON(resp) {
		if pretty, err := prettyPrint(body); err == nil {
			body = pretty
		}
	}
	_ = op
	return body, 0
}

func isJSON(resp *Response) bool {
	ct := resp.Headers["Content-Type"]
	return strings.Contains(ct, "json")
}

func prettyPrint(data []byte) ([]byte, error) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return json.MarshalIndent(v, "", "  ")
}

func errorMessage(resp *Response) string {
	switch resp.StatusCode {
	case 401:
		return "401 Unauthorized: check auth flags\n"
	case 403:
		return "403 Forbidden: insufficient permissions\n"
	case 404:
		return "404 Not Found\n"
	case 429:
		retryAfter := resp.Headers["Retry-After"]
		if retryAfter != "" {
			return fmt.Sprintf("429 Rate Limited: retry after %ss\n", retryAfter)
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
