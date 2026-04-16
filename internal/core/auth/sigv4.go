package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SigV4Config configures AWS Signature Version 4 request signing.
type SigV4Config struct {
	AccessKeyID    string
	SecretAccessKey string
	SessionToken   string // optional: for temporary credentials (STS)
	Region         string
	Service        string // e.g. "execute-api", "s3"
}

// SigV4Provider signs requests using AWS Signature Version 4.
type SigV4Provider struct {
	accessKey    string
	secretKey    string
	sessionToken string
	region       string
	service      string
}

// NewSigV4Provider builds a provider from the given config.
func NewSigV4Provider(cfg SigV4Config) (*SigV4Provider, error) {
	if cfg.AccessKeyID == "" {
		return nil, errors.New("sigv4: access_key_id is required")
	}
	if cfg.SecretAccessKey == "" {
		return nil, errors.New("sigv4: secret_access_key is required")
	}
	if cfg.Region == "" {
		return nil, errors.New("sigv4: region is required")
	}
	if cfg.Service == "" {
		cfg.Service = "execute-api"
	}
	return &SigV4Provider{
		accessKey:    cfg.AccessKeyID,
		secretKey:    cfg.SecretAccessKey,
		sessionToken: cfg.SessionToken,
		region:       cfg.Region,
		service:      cfg.Service,
	}, nil
}

// Name implements Provider.
func (p *SigV4Provider) Name() string { return "sigv4" }

// Apply implements Provider. Sets X-Amz-Date and X-Amz-Security-Token headers.
// For full SigV4 signing (which requires the complete request including body),
// use SignRequest at the HTTP transport layer instead.
func (p *SigV4Provider) Apply(_ context.Context, tgt *ApplyTarget) error {
	if tgt.Headers == nil {
		tgt.Headers = make(map[string]string)
	}
	tgt.Headers["X-Amz-Date"] = time.Now().UTC().Format("20060102T150405Z")
	if p.sessionToken != "" {
		tgt.Headers["X-Amz-Security-Token"] = p.sessionToken
	}
	return nil
}

// SignRequest signs a full http.Request with proper SigV4 including body hash.
func (p *SigV4Provider) SignRequest(req *http.Request, body []byte) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("X-Amz-Date", amzDate)
	if p.sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", p.sessionToken)
	}

	payloadHash := fmt.Sprintf("%x", sha256.Sum256(body))
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	canonicalHeaders, signedHeaders := sigv4CanonicalHeaders(req)
	canonicalRequest := strings.Join([]string{
		req.Method,
		sigv4CanonicalURI(req),
		req.URL.Query().Encode(),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, p.region, p.service)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		fmt.Sprintf("%x", sha256.Sum256([]byte(canonicalRequest))),
	}, "\n")

	signingKey := sigv4DeriveKey(p.secretKey, dateStamp, p.region, p.service)
	signature := fmt.Sprintf("%x", sigv4HMAC(signingKey, []byte(stringToSign)))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		p.accessKey, credentialScope, signedHeaders, signature,
	))
}

func sigv4DeriveKey(secret, dateStamp, region, service string) []byte {
	kDate := sigv4HMAC([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := sigv4HMAC(kDate, []byte(region))
	kService := sigv4HMAC(kRegion, []byte(service))
	return sigv4HMAC(kService, []byte("aws4_request"))
}

func sigv4HMAC(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sigv4CanonicalURI(req *http.Request) string {
	if req.URL.Path == "" {
		return "/"
	}
	return req.URL.Path
}

func sigv4CanonicalHeaders(req *http.Request) (canonical string, signed string) {
	names := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if req.Header.Get("X-Amz-Security-Token") != "" {
		names = append(names, "x-amz-security-token")
	}

	var parts []string
	for _, h := range names {
		val := ""
		if h == "host" {
			val = req.Host
			if val == "" {
				val = req.URL.Host
			}
		} else {
			val = req.Header.Get(h)
		}
		parts = append(parts, h+":"+strings.TrimSpace(val)+"\n")
	}
	return strings.Join(parts, ""), strings.Join(names, ";")
}
