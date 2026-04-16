package exec

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/apimount/apimount/internal/core/auth"
	"github.com/apimount/apimount/internal/core/spec"
)

// APIClient is the HTTP client with auth injection.
type APIClient struct {
	httpClient   *http.Client
	authInjector *auth.Injector // spec-aware static injector (Phase 1)
	authChain    *auth.Chain    // extensible provider chain (Phase 3)
}

// HTTPRequest is the internal HTTP request descriptor.
type HTTPRequest struct {
	Method      string
	URL         string
	Headers     map[string]string
	QueryParams map[string]string
	Body        []byte
	ContentType string
	OpSecurity  []spec.SecurityReq
}

// HTTPResponse is the internal HTTP response descriptor.
type HTTPResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
	Duration   time.Duration
}

// NewAPIClient creates a new API client with the Phase 1 static injector only.
// Use NewAPIClientWithChain for Phase 3 OAuth2 / multi-provider scenarios.
func NewAPIClient(timeout time.Duration, authCfg *auth.Config, schemes []spec.AuthScheme) *APIClient {
	return NewAPIClientWithChain(timeout, authCfg, schemes, nil)
}

// ClientOption configures optional APIClient behaviour.
type ClientOption func(*APIClient)

// WithTLSConfig sets a custom TLS configuration (e.g. for mTLS client certs).
func WithTLSConfig(cfg *tls.Config) ClientOption {
	return func(c *APIClient) {
		c.httpClient.Transport = &http.Transport{TLSClientConfig: cfg}
	}
}

// NewAPIClientWithChain creates an API client that runs both the spec-aware
// static injector (for Phase 1 bearer/basic/apikey flags) and the Phase 3
// Provider chain (for OAuth2 / extensible auth). Either may be nil.
func NewAPIClientWithChain(
	timeout time.Duration,
	authCfg *auth.Config,
	schemes []spec.AuthScheme,
	chain *auth.Chain,
	opts ...ClientOption,
) *APIClient {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if authCfg == nil {
		authCfg = &auth.Config{}
	}
	c := &APIClient{
		httpClient:   &http.Client{Timeout: timeout},
		authInjector: auth.NewInjector(authCfg, schemes),
		authChain:    chain,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Execute runs an HTTP request and returns the response.
func (c *APIClient) Execute(ctx context.Context, req *HTTPRequest) (*HTTPResponse, error) {
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

	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}
	if req.QueryParams == nil {
		req.QueryParams = make(map[string]string)
	}

	c.authInjector.Apply(req.OpSecurity, req.Headers, req.QueryParams)

	if c.authChain != nil {
		tgt := &auth.ApplyTarget{Headers: req.Headers, QueryParams: req.QueryParams}
		if err := c.authChain.Apply(ctx, tgt); err != nil {
			return nil, fmt.Errorf("auth chain: %w", err)
		}
	}

	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	if len(req.Body) > 0 {
		ct := req.ContentType
		if ct == "" {
			ct = "application/json"
		}
		httpReq.Header.Set("Content-Type", ct)
	}

	if httpReq.Header.Get("Accept") == "" {
		httpReq.Header.Set("Accept", "application/json")
	}

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	elapsed := time.Since(start)

	body, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024)) // 32 MB
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	headers := make(map[string]string)
	for k := range resp.Header {
		headers[k] = resp.Header.Get(k)
	}

	return &HTTPResponse{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Body:       body,
		Duration:   elapsed,
	}, nil
}
