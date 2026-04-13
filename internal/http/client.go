package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/apimount/apimount/internal/auth"
	"github.com/apimount/apimount/internal/spec"
)

// APIClient is the HTTP client with auth injection.
type APIClient struct {
	httpClient  *http.Client
	authInjector *auth.Injector
}

// Request is the internal HTTP request descriptor.
type Request struct {
	Method      string
	URL         string
	Headers     map[string]string
	QueryParams map[string]string
	Body        []byte
	ContentType string
	OpSecurity  []spec.SecurityReq
}

// Response is the internal HTTP response descriptor.
type Response struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
	Duration   time.Duration
}

// NewAPIClient creates a new API client.
func NewAPIClient(timeout time.Duration, authCfg *auth.Config, schemes []spec.AuthScheme) *APIClient {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &APIClient{
		httpClient:  &http.Client{Timeout: timeout},
		authInjector: auth.NewInjector(authCfg, schemes),
	}
}

// Execute runs an HTTP request and returns the response.
func (c *APIClient) Execute(ctx context.Context, req *Request) (*Response, error) {
	// Build URL with query params
	rawURL := req.URL
	if len(req.QueryParams) > 0 {
		u, err := url.Parse(rawURL)
		if err != nil {
			return nil, fmt.Errorf("invalid URL %q: %w", rawURL, err)
		}
		q := u.Query()
		for k, v := range req.QueryParams {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		rawURL = u.String()
	}

	var bodyReader io.Reader
	if len(req.Body) > 0 {
		bodyReader = bytes.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, rawURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("could not build HTTP request: %w", err)
	}

	// Set headers
	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}
	if req.QueryParams == nil {
		req.QueryParams = make(map[string]string)
	}

	// Inject auth
	c.authInjector.Apply(req.OpSecurity, req.Headers, req.QueryParams)

	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Content-Type
	if len(req.Body) > 0 {
		ct := req.ContentType
		if ct == "" {
			ct = "application/json"
		}
		httpReq.Header.Set("Content-Type", ct)
	}

	// Accept JSON by default
	if httpReq.Header.Get("Accept") == "" {
		httpReq.Header.Set("Accept", "application/json")
	}

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	headers := make(map[string]string)
	for k := range resp.Header {
		headers[k] = resp.Header.Get(k)
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Body:       body,
		Duration:   elapsed,
	}, nil
}
