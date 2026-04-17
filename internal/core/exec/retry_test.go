package exec

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apimount/apimount/internal/core/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryMiddleware_SuccessOnFirstAttempt(t *testing.T) {
	calls := 0
	handler := RetryMiddleware(RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond})(func(_ context.Context, _ *Request) (*Result, error) {
		calls++
		return &Result{Status: 200, Body: []byte("ok")}, nil
	})

	req := &Request{Op: &spec.Operation{Method: "GET"}}
	res, err := handler(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 200, res.Status)
	assert.Equal(t, 1, res.Attempts)
	assert.Equal(t, 1, calls)
}

func TestRetryMiddleware_RetriesOn503(t *testing.T) {
	calls := 0
	handler := RetryMiddleware(RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond})(func(_ context.Context, _ *Request) (*Result, error) {
		calls++
		if calls < 3 {
			return &Result{Status: 503}, nil
		}
		return &Result{Status: 200, Body: []byte("ok")}, nil
	})

	req := &Request{Op: &spec.Operation{Method: "GET"}}
	res, err := handler(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 200, res.Status)
	assert.Equal(t, 3, res.Attempts)
	assert.Equal(t, 3, calls)
}

func TestRetryMiddleware_ExhaustsAttempts(t *testing.T) {
	calls := 0
	handler := RetryMiddleware(RetryConfig{MaxAttempts: 2, BaseDelay: time.Millisecond})(func(_ context.Context, _ *Request) (*Result, error) {
		calls++
		return &Result{Status: 502}, nil
	})

	req := &Request{Op: &spec.Operation{Method: "GET"}}
	res, err := handler(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 502, res.Status)
	assert.Equal(t, 2, res.Attempts)
	assert.Equal(t, 2, calls)
}

func TestRetryMiddleware_NoRetryForPOST(t *testing.T) {
	calls := 0
	handler := RetryMiddleware(RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond})(func(_ context.Context, _ *Request) (*Result, error) {
		calls++
		return &Result{Status: 503}, nil
	})

	req := &Request{Op: &spec.Operation{Method: "POST"}}
	res, err := handler(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 503, res.Status)
	assert.Equal(t, 1, calls)
}

func TestRetryMiddleware_RetriesOnError(t *testing.T) {
	calls := 0
	handler := RetryMiddleware(RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond})(func(_ context.Context, _ *Request) (*Result, error) {
		calls++
		if calls < 2 {
			return nil, errors.New("connection reset")
		}
		return &Result{Status: 200}, nil
	})

	req := &Request{Op: &spec.Operation{Method: "GET"}}
	res, err := handler(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 200, res.Status)
	assert.Equal(t, 2, res.Attempts)
}

func TestRetryMiddleware_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	handler := RetryMiddleware(RetryConfig{MaxAttempts: 5, BaseDelay: 50 * time.Millisecond})(func(_ context.Context, _ *Request) (*Result, error) {
		calls++
		if calls == 1 {
			cancel()
		}
		return &Result{Status: 429}, nil
	})

	req := &Request{Op: &spec.Operation{Method: "GET"}}
	_, err := handler(ctx, req)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls)
}

func TestRetryMiddleware_NoRetryForNilOp(t *testing.T) {
	calls := 0
	handler := RetryMiddleware(RetryConfig{MaxAttempts: 3})(func(_ context.Context, _ *Request) (*Result, error) {
		calls++
		return &Result{Status: 503}, nil
	})

	req := &Request{}
	res, err := handler(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 503, res.Status)
	assert.Equal(t, 1, calls)
}

func TestRetryMiddleware_Defaults(t *testing.T) {
	cfg := RetryConfig{}.withDefaults()
	assert.Equal(t, 3, cfg.MaxAttempts)
	assert.Equal(t, 200*time.Millisecond, cfg.BaseDelay)
	assert.Equal(t, 5*time.Second, cfg.MaxDelay)
}

func TestBackoffWithJitter(t *testing.T) {
	for i := 0; i < 100; i++ {
		d := backoffWithJitter(100*time.Millisecond, 5*time.Second, 1)
		assert.GreaterOrEqual(t, d, time.Duration(0))
		assert.LessOrEqual(t, d, 200*time.Millisecond)
	}
}

func TestIsRetryableStatus(t *testing.T) {
	retryable := []int{429, 502, 503, 504}
	nonRetryable := []int{200, 201, 301, 400, 401, 403, 404, 500}

	for _, s := range retryable {
		assert.True(t, isRetryableStatus(s), "expected %d to be retryable", s)
	}
	for _, s := range nonRetryable {
		assert.False(t, isRetryableStatus(s), "expected %d to NOT be retryable", s)
	}
}
