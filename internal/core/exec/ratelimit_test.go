package exec

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apimount/apimount/internal/core/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimitMiddleware_AllowsBurst(t *testing.T) {
	handler := RateLimitMiddleware(RateLimitConfig{RequestsPerSecond: 100, Burst: 5})(
		func(_ context.Context, _ *Request) (*Result, error) {
			return &Result{Status: 200}, nil
		},
	)

	req := &Request{Op: &spec.Operation{Method: "GET", Path: "/test"}}
	for i := 0; i < 5; i++ {
		res, err := handler(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, 200, res.Status)
	}
}

func TestRateLimitMiddleware_ThrottlesAfterBurst(t *testing.T) {
	var calls atomic.Int32
	handler := RateLimitMiddleware(RateLimitConfig{RequestsPerSecond: 1000, Burst: 2})(
		func(_ context.Context, _ *Request) (*Result, error) {
			calls.Add(1)
			return &Result{Status: 200}, nil
		},
	)

	req := &Request{Op: &spec.Operation{Method: "GET", Path: "/test"}}
	start := time.Now()
	for i := 0; i < 4; i++ {
		_, err := handler(context.Background(), req)
		require.NoError(t, err)
	}
	elapsed := time.Since(start)
	assert.Equal(t, int32(4), calls.Load())
	assert.GreaterOrEqual(t, elapsed, time.Millisecond)
}

func TestRateLimitMiddleware_RespectsContextCancel(t *testing.T) {
	handler := RateLimitMiddleware(RateLimitConfig{RequestsPerSecond: 1, Burst: 1})(
		func(_ context.Context, _ *Request) (*Result, error) {
			return &Result{Status: 200}, nil
		},
	)

	req := &Request{Op: &spec.Operation{Method: "GET", Path: "/test"}}
	// Consume the burst
	_, _ = handler(context.Background(), req)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := handler(ctx, req)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRateLimitMiddleware_DrainOnRetryAfter(t *testing.T) {
	calls := 0
	handler := RateLimitMiddleware(RateLimitConfig{RequestsPerSecond: 1000, Burst: 10})(
		func(_ context.Context, _ *Request) (*Result, error) {
			calls++
			if calls == 1 {
				return &Result{
					Status:  429,
					Headers: map[string]string{"Retry-After": "1"},
				}, nil
			}
			return &Result{Status: 200}, nil
		},
	)

	req := &Request{Op: &spec.Operation{Method: "GET", Path: "/test"}}
	res, err := handler(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 429, res.Status)
}

func TestRateLimitMiddleware_NilOpPassesThrough(t *testing.T) {
	handler := RateLimitMiddleware(RateLimitConfig{})(
		func(_ context.Context, _ *Request) (*Result, error) {
			return &Result{Status: 200}, nil
		},
	)
	res, err := handler(context.Background(), &Request{})
	require.NoError(t, err)
	assert.Equal(t, 200, res.Status)
}

func TestRateLimitConfig_Defaults(t *testing.T) {
	cfg := RateLimitConfig{}.withDefaults()
	assert.Equal(t, float64(10), cfg.RequestsPerSecond)
	assert.Equal(t, 20, cfg.Burst)
}

func TestRetryAfterDuration(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    time.Duration
	}{
		{"nil headers", nil, 0},
		{"no header", map[string]string{}, 0},
		{"seconds", map[string]string{"Retry-After": "5"}, 5 * time.Second},
		{"invalid", map[string]string{"Retry-After": "bogus"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, retryAfterDuration(tt.headers))
		})
	}
}
