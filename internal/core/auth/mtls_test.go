package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTestCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-client"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	certPath = filepath.Join(dir, "client.crt")
	keyPath = filepath.Join(dir, "client.key")

	certFile, err := os.Create(certPath)
	require.NoError(t, err)
	pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certFile.Close()

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	keyFile, err := os.Create(keyPath)
	require.NoError(t, err)
	pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	keyFile.Close()

	return certPath, keyPath
}

func TestMTLSProvider_LoadsKeypair(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := generateTestCert(t, dir)

	provider, err := NewMTLSProvider(MTLSConfig{
		CertFile: certPath,
		KeyFile:  keyPath,
	})
	require.NoError(t, err)
	assert.Equal(t, "mtls", provider.Name())

	tlsCfg := provider.TLSConfig()
	require.NotNil(t, tlsCfg)
	assert.Len(t, tlsCfg.Certificates, 1)
	assert.Nil(t, tlsCfg.RootCAs, "no custom CA should be set")
}

func TestMTLSProvider_WithCA(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := generateTestCert(t, dir)

	caPath := filepath.Join(dir, "ca.crt")
	certPEM, err := os.ReadFile(certPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(caPath, certPEM, 0o600))

	provider, err := NewMTLSProvider(MTLSConfig{
		CertFile: certPath,
		KeyFile:  keyPath,
		CAFile:   caPath,
	})
	require.NoError(t, err)
	assert.NotNil(t, provider.TLSConfig().RootCAs)
}

func TestMTLSProvider_ValidationErrors(t *testing.T) {
	_, err := NewMTLSProvider(MTLSConfig{})
	assert.ErrorContains(t, err, "cert-file")

	_, err = NewMTLSProvider(MTLSConfig{CertFile: "x"})
	assert.ErrorContains(t, err, "key-file")

	_, err = NewMTLSProvider(MTLSConfig{CertFile: "nonexistent.crt", KeyFile: "nonexistent.key"})
	assert.ErrorContains(t, err, "load keypair")
}

func TestMTLSProvider_ApplyIsNoOp(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := generateTestCert(t, dir)
	provider, err := NewMTLSProvider(MTLSConfig{CertFile: certPath, KeyFile: keyPath})
	require.NoError(t, err)

	tgt := &ApplyTarget{Headers: map[string]string{"Existing": "header"}}
	require.NoError(t, provider.Apply(context.Background(), tgt))
	assert.Equal(t, map[string]string{"Existing": "header"}, tgt.Headers, "mTLS Apply should not modify headers")
}
