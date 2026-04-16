package auth

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSigV4Provider_Validate(t *testing.T) {
	_, err := NewSigV4Provider(SigV4Config{})
	assert.ErrorContains(t, err, "access_key_id")

	_, err = NewSigV4Provider(SigV4Config{AccessKeyID: "x"})
	assert.ErrorContains(t, err, "secret_access_key")

	_, err = NewSigV4Provider(SigV4Config{AccessKeyID: "x", SecretAccessKey: "y"})
	assert.ErrorContains(t, err, "region")
}

func TestSigV4Provider_DefaultService(t *testing.T) {
	p, err := NewSigV4Provider(SigV4Config{
		AccessKeyID:    "AKID",
		SecretAccessKey: "secret",
		Region:         "us-east-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "sigv4", p.Name())
	assert.Equal(t, "execute-api", p.service)
}

func TestSigV4Provider_ApplySetsHeaders(t *testing.T) {
	p, err := NewSigV4Provider(SigV4Config{
		AccessKeyID:    "AKID",
		SecretAccessKey: "secret",
		Region:         "us-west-2",
		Service:        "s3",
		SessionToken:   "session-tok",
	})
	require.NoError(t, err)

	tgt := &ApplyTarget{}
	require.NoError(t, p.Apply(context.Background(), tgt))
	assert.NotEmpty(t, tgt.Headers["X-Amz-Date"])
	assert.Equal(t, "session-tok", tgt.Headers["X-Amz-Security-Token"])
}

func TestSigV4Provider_SignRequest(t *testing.T) {
	p, err := NewSigV4Provider(SigV4Config{
		AccessKeyID:    "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Region:         "us-east-1",
		Service:        "s3",
	})
	require.NoError(t, err)

	req, _ := http.NewRequest("GET", "https://s3.amazonaws.com/mybucket/mykey", nil)
	p.SignRequest(req, nil)

	authHeader := req.Header.Get("Authorization")
	assert.Contains(t, authHeader, "AWS4-HMAC-SHA256")
	assert.Contains(t, authHeader, "Credential=AKIAIOSFODNN7EXAMPLE/")
	assert.Contains(t, authHeader, "SignedHeaders=host;x-amz-content-sha256;x-amz-date")
	assert.Contains(t, authHeader, "Signature=")
	assert.NotEmpty(t, req.Header.Get("X-Amz-Date"))
	assert.NotEmpty(t, req.Header.Get("X-Amz-Content-Sha256"))
}

func TestSigV4_DeriveKey(t *testing.T) {
	key := sigv4DeriveKey("secret", "20230101", "us-east-1", "s3")
	assert.Len(t, key, 32, "HMAC-SHA256 output should be 32 bytes")
}
