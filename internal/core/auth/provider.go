package auth

import (
	"context"
)

// ApplyTarget is the subset of an HTTP request an auth Provider is allowed to
// mutate. It is intentionally narrow — a Provider must not see or touch the
// request body, URL path, or method. This keeps auth providers composable and
// keeps the core/exec HTTPRequest type an implementation detail the auth
// package does not depend on (avoids an import cycle).
type ApplyTarget struct {
	Headers     map[string]string
	QueryParams map[string]string
}

// Provider is the Phase 3 auth contract. Every auth mechanism — static
// bearer/basic/apikey, OAuth2 client-credentials, SigV4, mTLS headers —
// implements this single interface. Providers compose into a Chain.
type Provider interface {
	// Name is used in logs and error messages. Should be short and stable.
	Name() string

	// Apply mutates the request to carry credentials. Returning an error
	// aborts the request; the chain stops and the caller sees the error.
	// A Provider that has nothing to contribute to this request (e.g. a
	// scheme-specific provider on an operation that doesn't use it) must
	// return nil without mutating the target.
	Apply(ctx context.Context, tgt *ApplyTarget) error
}

// Chain runs Providers in order. It is itself a Provider, so chains compose.
// A nil or empty Chain is a no-op.
type Chain struct {
	providers []Provider
}

// NewChain builds a Chain from the given providers. Nils are dropped.
func NewChain(providers ...Provider) *Chain {
	c := &Chain{}
	for _, p := range providers {
		if p != nil {
			c.providers = append(c.providers, p)
		}
	}
	return c
}

// Name returns a stable identifier for the chain.
func (c *Chain) Name() string { return "chain" }

// Apply runs each provider in order. The first error short-circuits the chain.
func (c *Chain) Apply(ctx context.Context, tgt *ApplyTarget) error {
	if c == nil {
		return nil
	}
	for _, p := range c.providers {
		if err := p.Apply(ctx, tgt); err != nil {
			return err
		}
	}
	return nil
}

// Len returns the number of providers in the chain (mostly for tests).
func (c *Chain) Len() int {
	if c == nil {
		return 0
	}
	return len(c.providers)
}

// StaticProvider wraps the existing per-spec Injector (Bearer/Basic/API-key)
// so v1 credentials flow through the new Chain-based pipeline unchanged. It
// exists for backwards compatibility — new code should prefer scheme-specific
// providers (OAuth2CC, SigV4, etc.).
type StaticProvider struct {
	inj *Injector
}

// NewStaticProvider wraps an Injector as a Provider.
func NewStaticProvider(inj *Injector) *StaticProvider {
	return &StaticProvider{inj: inj}
}

// Name implements Provider.
func (p *StaticProvider) Name() string { return "static" }

// Apply implements Provider. It calls the wrapped Injector with no operation
// security context — used for direct credential injection. The per-operation
// security resolution still happens at the APIClient layer for now; moving
// that to the Provider interface is a Phase 4 cleanup.
func (p *StaticProvider) Apply(_ context.Context, tgt *ApplyTarget) error {
	if p == nil || p.inj == nil {
		return nil
	}
	if tgt.Headers == nil {
		tgt.Headers = make(map[string]string)
	}
	if tgt.QueryParams == nil {
		tgt.QueryParams = make(map[string]string)
	}
	p.inj.ApplyDirect(tgt.Headers, tgt.QueryParams)
	return nil
}
