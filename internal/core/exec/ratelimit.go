package exec

import (
	"context"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimitConfig controls the per-host token-bucket rate limiter.
type RateLimitConfig struct {
	RequestsPerSecond float64 // steady-state rate (default 10)
	Burst             int     // max burst size (default 20)
}

func (c RateLimitConfig) withDefaults() RateLimitConfig {
	if c.RequestsPerSecond <= 0 {
		c.RequestsPerSecond = 10
	}
	if c.Burst <= 0 {
		c.Burst = 20
	}
	return c
}

// RateLimitMiddleware enforces per-host rate limits using a token bucket.
// It honours Retry-After and X-RateLimit-* response headers to dynamically
// adjust pacing.
func RateLimitMiddleware(cfg RateLimitConfig) Middleware {
	cfg = cfg.withDefaults()
	buckets := &bucketRegistry{
		buckets: make(map[string]*tokenBucket),
		cfg:     cfg,
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request) (*Result, error) {
			host := hostFromOp(req)
			if host == "" {
				return next(ctx, req)
			}

			bucket := buckets.get(host)
			if err := bucket.wait(ctx); err != nil {
				return nil, err
			}

			result, err := next(ctx, req)
			if result != nil {
				bucket.adjustFromHeaders(result.Headers)
				if result.Status == 429 || result.Status == 503 {
					if ra := retryAfterDuration(result.Headers); ra > 0 {
						bucket.drain(ra)
					}
				}
			}
			return result, err
		}
	}
}

func hostFromOp(req *Request) string {
	if req == nil || req.Op == nil {
		return ""
	}
	u, err := url.Parse(req.Op.Path)
	if err != nil || u.Host == "" {
		return "default"
	}
	return strings.ToLower(u.Host)
}

type bucketRegistry struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	cfg     RateLimitConfig
}

func (r *bucketRegistry) get(host string) *tokenBucket {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.buckets[host]
	if !ok {
		b = newTokenBucket(r.cfg.RequestsPerSecond, r.cfg.Burst)
		r.buckets[host] = b
	}
	return b
}

type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	max      float64
	rate     float64 // tokens per second
	lastFill time.Time
	drainEnd time.Time // if set, bucket is drained until this time
}

func newTokenBucket(rate float64, burst int) *tokenBucket {
	return &tokenBucket{
		tokens:   float64(burst),
		max:      float64(burst),
		rate:     rate,
		lastFill: time.Now(),
	}
}

func (b *tokenBucket) wait(ctx context.Context) error {
	for {
		b.mu.Lock()
		now := time.Now()

		if now.Before(b.drainEnd) {
			waitDur := b.drainEnd.Sub(now)
			b.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(waitDur):
			}
			continue
		}

		elapsed := now.Sub(b.lastFill).Seconds()
		b.tokens = math.Min(b.max, b.tokens+elapsed*b.rate)
		b.lastFill = now

		if b.tokens >= 1 {
			b.tokens--
			b.mu.Unlock()
			return nil
		}

		waitDur := time.Duration((1 - b.tokens) / b.rate * float64(time.Second))
		b.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDur):
		}
	}
}

func (b *tokenBucket) drain(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.drainEnd = time.Now().Add(d)
	b.tokens = 0
}

func (b *tokenBucket) adjustFromHeaders(headers map[string]string) {
	if headers == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if limitStr, ok := headers["X-Ratelimit-Limit"]; ok {
		if limit, err := strconv.ParseFloat(limitStr, 64); err == nil && limit > 0 {
			b.rate = limit
			b.max = limit * 2
		}
	}
}

func retryAfterDuration(headers map[string]string) time.Duration {
	if headers == nil {
		return 0
	}
	ra, ok := headers["Retry-After"]
	if !ok {
		return 0
	}
	if secs, err := strconv.Atoi(ra); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := time.Parse(time.RFC1123, ra); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}
