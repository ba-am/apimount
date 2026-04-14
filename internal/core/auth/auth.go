// Package auth provides pluggable authentication injection for outbound HTTP requests.
package auth

import (
	"encoding/base64"
	"fmt"

	"github.com/apimount/apimount/internal/core/spec"
)

// Config holds the auth credentials provided by the user.
type Config struct {
	Bearer      string // Bearer token
	Basic       string // "user:password"
	APIKey      string // raw API key value
	APIKeyParam string // param name override (if not in spec)
}

// Injector applies auth headers/params to a request based on the spec's security schemes
// and the user-supplied Config.
type Injector struct {
	cfg     *Config
	schemes []spec.AuthScheme
}

// NewInjector creates a new auth injector.
func NewInjector(cfg *Config, schemes []spec.AuthScheme) *Injector {
	if cfg == nil {
		cfg = &Config{}
	}
	return &Injector{cfg: cfg, schemes: schemes}
}

// Apply injects auth into the given headers and queryParams maps.
func (inj *Injector) Apply(
	operationSecurity []spec.SecurityReq,
	headers map[string]string,
	queryParams map[string]string,
) {
	schemasToApply := inj.resolveSchemes(operationSecurity)

	for _, schemeName := range schemasToApply {
		scheme := inj.findScheme(schemeName)
		inj.applyScheme(scheme, schemeName, headers, queryParams)
	}

	if len(schemasToApply) == 0 {
		inj.applyDirectCredentials(headers, queryParams)
	}
}

// ApplyDirect injects auth credentials directly without spec scheme resolution.
func (inj *Injector) ApplyDirect(headers map[string]string, queryParams map[string]string) {
	inj.applyDirectCredentials(headers, queryParams)
}

func (inj *Injector) resolveSchemes(opSecurity []spec.SecurityReq) []string {
	if len(opSecurity) > 0 {
		var names []string
		for _, req := range opSecurity {
			for name := range req {
				names = append(names, name)
			}
		}
		return names
	}
	var names []string
	for _, s := range inj.schemes {
		names = append(names, s.Name)
	}
	return names
}

func (inj *Injector) findScheme(name string) *spec.AuthScheme {
	for i, s := range inj.schemes {
		if s.Name == name {
			return &inj.schemes[i]
		}
	}
	return nil
}

func (inj *Injector) applyScheme(
	scheme *spec.AuthScheme,
	schemeName string,
	headers map[string]string,
	queryParams map[string]string,
) {
	_ = schemeName
	if scheme == nil {
		inj.applyDirectCredentials(headers, queryParams)
		return
	}

	switch scheme.Type {
	case "http":
		switch scheme.Scheme {
		case "bearer":
			if inj.cfg.Bearer != "" {
				headers["Authorization"] = "Bearer " + inj.cfg.Bearer
			}
		case "basic":
			if inj.cfg.Basic != "" {
				headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(inj.cfg.Basic))
			}
		}
	case "apiKey":
		key := inj.cfg.APIKey
		param := scheme.Param
		if inj.cfg.APIKeyParam != "" {
			param = inj.cfg.APIKeyParam
		}
		if key == "" || param == "" {
			return
		}
		switch scheme.In {
		case "header":
			headers[param] = key
		case "query":
			queryParams[param] = key
		}
	}
}

func (inj *Injector) applyDirectCredentials(headers map[string]string, queryParams map[string]string) {
	if inj.cfg.Bearer != "" {
		headers["Authorization"] = "Bearer " + inj.cfg.Bearer
	} else if inj.cfg.Basic != "" {
		headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(inj.cfg.Basic))
	}
	if inj.cfg.APIKey != "" {
		param := inj.cfg.APIKeyParam
		if param == "" {
			for _, s := range inj.schemes {
				if s.Type == "apiKey" {
					param = s.Param
					if s.In == "query" {
						queryParams[param] = inj.cfg.APIKey
						return
					}
					break
				}
			}
			if param == "" {
				param = "X-API-Key"
			}
		}
		headers[param] = inj.cfg.APIKey
	}
}

// HeaderValue returns the Authorization header value for documentation purposes.
func HeaderValue(cfg *Config) string {
	if cfg.Bearer != "" {
		return fmt.Sprintf("Bearer %s", cfg.Bearer)
	}
	if cfg.Basic != "" {
		return fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(cfg.Basic)))
	}
	return ""
}
