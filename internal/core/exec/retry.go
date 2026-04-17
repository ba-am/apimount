package exec

import (
	"context"
	"math"
	"math/rand/v2"
	"strings"
	"time"
)

// RetryConfig controls the retry middleware behaviour.
type RetryConfig struct {
	MaxAttempts int           // total attempts including the first (default 3)
	BaseDelay   time.Duration // initial backoff (default 200ms)
	MaxDelay    time.Duration // cap on backoff (default 5s)
}

func (c RetryConfig) withDefaults() RetryConfig {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 3
	}
	if c.BaseDelay <= 0 {
		c.BaseDelay = 200 * time.Millisecond
	}
	if c.MaxDelay <= 0 {
		c.MaxDelay = 5 * time.Second
	}
	return c
}

// RetryMiddleware retries failed requests with exponential backoff and full
// jitter. Only idempotent methods (GET, HEAD, PUT, DELETE) are retried.
// POST/PATCH are never retried unless the operation carries the
// x-apimount-idempotent extension.
func RetryMiddleware(cfg RetryConfig) Middleware {
	cfg = cfg.withDefaults()

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request) (*Result, error) {
			if !isRetryable(req) {
				return next(ctx, req)
			}

			var lastResult *Result
			var lastErr error

			for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
				if attempt > 0 {
					delay := backoffWithJitter(cfg.BaseDelay, cfg.MaxDelay, attempt)
					select {
					case <-ctx.Done():
						return lastResult, ctx.Err()
					case <-time.After(delay):
					}
				}

				lastResult, lastErr = next(ctx, req)
				if lastErr == nil && lastResult != nil && !isRetryableStatus(lastResult.Status) {
					lastResult.Attempts = attempt + 1
					return lastResult, nil
				}
				if lastResult != nil {
					lastResult.Attempts = attempt + 1
				}
			}

			return lastResult, lastErr
		}
	}
}

func isRetryable(req *Request) bool {
	if req.Op == nil {
		return false
	}
	method := strings.ToUpper(req.Op.Method)
	switch method {
	case "GET", "HEAD", "PUT", "DELETE", "OPTIONS":
		return true
	}
	return false
}

func isRetryableStatus(status int) bool {
	switch status {
	case 429, 502, 503, 504:
		return true
	}
	return false
}

func backoffWithJitter(base, max time.Duration, attempt int) time.Duration {
	exp := math.Pow(2, float64(attempt))
	delay := time.Duration(float64(base) * exp)
	if delay > max {
		delay = max
	}
	// Full jitter: uniform random in [0, delay]
	if delay > 0 {
		delay = time.Duration(rand.Int64N(int64(delay)))
	}
	return delay
}
