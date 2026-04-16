// Package exec is the HTTP execution pipeline for apimount.
// Business logic lives here; frontends call Do() and get a Result back.
package exec

import (
	"context"

	"github.com/apimount/apimount/internal/core/spec"
)

// Request is the normalised input to the executor pipeline.
type Request struct {
	Op          *spec.Operation
	PathParams  map[string]string
	QueryParams map[string]string
	Headers     map[string]string
	Body        []byte
	ReadOnly    bool
}

// Result is the normalised output of the executor pipeline.
type Result struct {
	Status     int
	Headers    map[string]string
	Body       []byte
	FromCache  bool
	DurationMs int64
	Attempts   int
}

// Handler is the terminal function in the middleware chain — it executes an HTTP request.
type Handler func(ctx context.Context, req *Request) (*Result, error)

// Middleware wraps a Handler, enabling cross-cutting concerns (auth, retry, cache, etc.).
// Every middleware is a pure function: no side effects on the Executor struct.
type Middleware func(next Handler) Handler

// chain builds a single Handler from an ordered list of middlewares and a base handler.
// Middlewares are applied outermost-first: chain[0] wraps chain[1] wraps ... wraps base.
func chain(base Handler, middlewares []Middleware) Handler {
	h := base
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
