package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

// MTLSConfig configures mutual TLS client authentication.
type MTLSConfig struct {
	CertFile string // path to PEM-encoded client certificate
	KeyFile  string // path to PEM-encoded private key
	CAFile   string // optional: path to PEM-encoded CA bundle for server verification
}

// MTLSProvider holds a parsed TLS certificate pair for use in HTTP transports.
// Unlike other providers, mTLS doesn't inject headers — it configures the
// TLS handshake. The provider exposes TLSConfig() for the HTTP client to use.
type MTLSProvider struct {
	tlsCfg *tls.Config
}

// NewMTLSProvider loads the client certificate and key, optionally adding
// a custom CA pool.
func NewMTLSProvider(cfg MTLSConfig) (*MTLSProvider, error) {
	if cfg.CertFile == "" {
		return nil, errors.New("mtls: cert-file is required")
	}
	if cfg.KeyFile == "" {
		return nil, errors.New("mtls: key-file is required")
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("mtls: load keypair: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if cfg.CAFile != "" {
		caPEM, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("mtls: read CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, errors.New("mtls: CA file contains no valid certificates")
		}
		tlsCfg.RootCAs = pool
	}

	return &MTLSProvider{tlsCfg: tlsCfg}, nil
}

// Name implements Provider.
func (p *MTLSProvider) Name() string { return "mtls" }

// TLSConfig returns the configured TLS config for use by the HTTP transport.
func (p *MTLSProvider) TLSConfig() *tls.Config { return p.tlsCfg }

// Apply is a no-op for mTLS — authentication happens at the TLS layer,
// not via HTTP headers. The provider satisfies the Provider interface so it
// can participate in the Chain for name/detection purposes, but the actual
// TLS config is consumed by the HTTP client directly via TLSConfig().
func (p *MTLSProvider) Apply(_ context.Context, _ *ApplyTarget) error {
	return nil
}
