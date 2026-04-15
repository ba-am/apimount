// Package secret provides a pluggable SecretProvider interface for resolving
// credentials from external sources without hard-coding them into profiles or
// shell history.
//
// A secret reference is a string of the form "<scheme>:<body>". Supported
// schemes in Phase 3:
//
//	env:VAR       — read os.Getenv("VAR")
//	file:/path    — read file contents (trailing newline trimmed), file must be chmod 0600
//	literal:xxx   — the literal string "xxx" (explicit opt-in for non-indirect values)
//
// Any ref with no recognised scheme is treated as a literal for backwards
// compatibility with existing CLI flags.
package secret

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// Provider resolves a single scheme ("env", "file", ...) into a string value.
type Provider interface {
	Scheme() string
	Get(ctx context.Context, body string) (string, error)
}

// Registry holds the set of configured providers and resolves refs against them.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry returns a Registry pre-populated with the built-in env, file,
// and literal providers.
func NewRegistry() *Registry {
	r := &Registry{providers: make(map[string]Provider)}
	r.Register(EnvProvider{})
	r.Register(FileProvider{})
	r.Register(LiteralProvider{})
	return r
}

// Register adds or replaces a provider.
func (r *Registry) Register(p Provider) {
	r.providers[p.Scheme()] = p
}

// Resolve turns a ref like "env:GITHUB_TOKEN" into its underlying value.
// An empty ref returns ("", nil). A ref with no recognised scheme is treated
// as a literal.
func (r *Registry) Resolve(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	scheme, body, ok := strings.Cut(ref, ":")
	if !ok {
		return ref, nil
	}
	p, exists := r.providers[scheme]
	if !exists {
		return ref, nil
	}
	return p.Get(ctx, body)
}

// EnvProvider reads from process environment variables.
type EnvProvider struct{}

func (EnvProvider) Scheme() string { return "env" }
func (EnvProvider) Get(_ context.Context, name string) (string, error) {
	if name == "" {
		return "", errors.New("env: empty variable name")
	}
	val, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("env: variable %q not set", name)
	}
	return val, nil
}

// FileProvider reads the entire contents of a file. The file must be chmod
// 0600 (owner read/write only) on Unix systems — this is the same guarantee
// docker login / kubectl use for their credential stores. Trailing newline is
// trimmed.
type FileProvider struct{}

func (FileProvider) Scheme() string { return "file" }
func (FileProvider) Get(_ context.Context, path string) (string, error) {
	if path == "" {
		return "", errors.New("file: empty path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("file: stat %s: %w", path, err)
	}
	if runtime.GOOS != "windows" {
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			return "", fmt.Errorf("file: %s has permissions %#o; must be 0600 (chmod 600 %s)", path, mode, path)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("file: read %s: %w", path, err)
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

// LiteralProvider returns its body unchanged. Use "literal:abc" to express
// "this really is the credential, not an indirection".
type LiteralProvider struct{}

func (LiteralProvider) Scheme() string                             { return "literal" }
func (LiteralProvider) Get(_ context.Context, body string) (string, error) { return body, nil }
