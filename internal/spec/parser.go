package spec

import (
	"context"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// ParsedSpec is the normalised internal representation of an OpenAPI spec.
type ParsedSpec struct {
	Title       string
	Version     string
	BaseURL     string
	Operations  []Operation
	AuthSchemes []AuthScheme
}

// Operation represents one HTTP operation from the spec.
type Operation struct {
	OperationID string
	Method      string
	Path        string
	Summary     string
	Description string
	Parameters  []Parameter
	RequestBody *RequestBody
	Responses   map[int]Response
	Security    []SecurityReq
	Tags        []string
}

// Parameter is a single path, query, or header parameter.
type Parameter struct {
	Name        string
	In          string
	Required    bool
	Schema      Schema
	Description string
}

// RequestBody describes the body for POST/PUT/PATCH.
type RequestBody struct {
	Required    bool
	Description string
	ContentType string
	Schema      Schema
}

// Response is a simplified response descriptor.
type Response struct {
	Description string
	ContentType string
	Schema      Schema
}

// Schema is a minimal JSON Schema subset.
type Schema struct {
	Type        string
	Format      string
	Properties  map[string]Schema
	Items       *Schema
	Description string
	Example     interface{}
	Required    []string
	Enum        []interface{}
}

// AuthScheme describes one security scheme from the spec.
type AuthScheme struct {
	Name   string
	Type   string
	Scheme string
	In     string
	Param  string
}

// SecurityReq maps scheme name → required scopes.
type SecurityReq map[string][]string

// Parse loads raw spec bytes and returns a ParsedSpec.
func Parse(data []byte, sourcePath string) (*ParsedSpec, error) {
	// Reject Swagger 2.0 before attempting to parse (kin-openapi only handles 3.x)
	if isSwagger2(data) {
		return nil, fmt.Errorf("unsupported spec version: 2.0 (Swagger). Only OpenAPI 3.x is supported")
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("invalid OpenAPI spec: %w", err)
	}

	if err := doc.Validate(context.Background()); err != nil {
		// Non-fatal: some valid specs fail strict validation
		_ = err
	}

	ps := &ParsedSpec{}

	// Info
	if doc.Info != nil {
		ps.Title = doc.Info.Title
		ps.Version = doc.Info.Version
	}

	// Base URL from first server
	if len(doc.Servers) > 0 {
		ps.BaseURL = resolveServerURL(doc.Servers[0])
	}

	// Auth schemes
	if doc.Components != nil {
		for name, scheme := range doc.Components.SecuritySchemes {
			ps.AuthSchemes = append(ps.AuthSchemes, parseAuthScheme(name, scheme.Value))
		}
	}

	// Operations
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	for path, item := range doc.Paths.Map() {
		for _, method := range methods {
			op := item.GetOperation(method)
			if op == nil {
				continue
			}

			parsed := parseOperation(method, path, op)
			ps.Operations = append(ps.Operations, parsed)
		}
	}

	return ps, nil
}

func resolveServerURL(server *openapi3.Server) string {
	u := server.URL
	for k, v := range server.Variables {
		if v.Default != "" {
			u = strings.ReplaceAll(u, "{"+k+"}", v.Default)
		}
	}
	// Strip trailing slash
	return strings.TrimRight(u, "/")
}

func parseOperation(method, path string, op *openapi3.Operation) Operation {
	out := Operation{
		Method:      method,
		Path:        path,
		Summary:     op.Summary,
		Description: op.Description,
		Tags:        op.Tags,
		Responses:   make(map[int]Response),
	}

	// OperationID — generate if missing
	if op.OperationID != "" {
		out.OperationID = op.OperationID
	} else {
		out.OperationID = generateOperationID(method, path)
	}

	// Parameters
	for _, pRef := range op.Parameters {
		if pRef.Value == nil {
			continue
		}
		p := pRef.Value
		param := Parameter{
			Name:        p.Name,
			In:          p.In,
			Required:    p.Required,
			Description: p.Description,
		}
		if p.Schema != nil && p.Schema.Value != nil {
			param.Schema = parseSchema(p.Schema.Value)
		}
		out.Parameters = append(out.Parameters, param)
	}

	// Request body
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		rb := op.RequestBody.Value
		reqBody := &RequestBody{
			Required:    rb.Required,
			Description: rb.Description,
		}
		for ct, mt := range rb.Content {
			reqBody.ContentType = ct
			if mt.Schema != nil && mt.Schema.Value != nil {
				reqBody.Schema = parseSchema(mt.Schema.Value)
			}
			break // take first content type
		}
		out.RequestBody = reqBody
	}

	// Responses
	if op.Responses != nil {
		for code, rRef := range op.Responses.Map() {
			if rRef.Value == nil {
				continue
			}
			r := rRef.Value
			desc := ""
			if r.Description != nil {
				desc = *r.Description
			}
			resp := Response{
				Description: desc,
			}
			for ct, mt := range r.Content {
				resp.ContentType = ct
				if mt.Schema != nil && mt.Schema.Value != nil {
					resp.Schema = parseSchema(mt.Schema.Value)
				}
				break
			}
			statusCode := parseStatusCode(code)
			if statusCode > 0 {
				out.Responses[statusCode] = resp
			}
		}
	}

	// Security
	if op.Security != nil {
		for _, sr := range *op.Security {
			req := SecurityReq{}
			for name, scopes := range sr {
				req[name] = scopes
			}
			out.Security = append(out.Security, req)
		}
	}

	return out
}

func parseSchema(s *openapi3.Schema) Schema {
	if s == nil {
		return Schema{}
	}
	schemaType := ""
	if s.Type != nil && len(*s.Type) > 0 {
		schemaType = (*s.Type)[0]
	}
	out := Schema{
		Type:        schemaType,
		Format:      s.Format,
		Description: s.Description,
		Example:     s.Example,
	}

	if len(s.Enum) > 0 {
		out.Enum = s.Enum
	}

	if len(s.Required) > 0 {
		out.Required = s.Required
	}

	if len(s.Properties) > 0 {
		out.Properties = make(map[string]Schema)
		for k, v := range s.Properties {
			if v.Value != nil {
				out.Properties[k] = parseSchema(v.Value)
			}
		}
	}

	if s.Items != nil && s.Items.Value != nil {
		items := parseSchema(s.Items.Value)
		out.Items = &items
	}

	return out
}

func parseAuthScheme(name string, s *openapi3.SecurityScheme) AuthScheme {
	if s == nil {
		return AuthScheme{Name: name}
	}
	as := AuthScheme{
		Name: name,
		Type: s.Type,
	}
	switch s.Type {
	case "http":
		as.Scheme = s.Scheme
	case "apiKey":
		as.In = s.In
		as.Param = s.Name
	}
	return as
}

func generateOperationID(method, path string) string {
	// e.g. GET /pets/{petId} → get_pets_petid
	seg := strings.ToLower(method) + "_" + strings.ToLower(path)
	seg = strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(seg)
	seg = strings.Trim(seg, "_")
	// collapse double underscores
	for strings.Contains(seg, "__") {
		seg = strings.ReplaceAll(seg, "__", "_")
	}
	return seg
}

func parseStatusCode(s string) int {
	if s == "default" {
		return 0
	}
	var code int
	_, err := fmt.Sscanf(s, "%d", &code)
	if err != nil {
		return 0
	}
	return code
}

// isSwagger2 detects Swagger 2.0 specs by checking for the "swagger" key.
func isSwagger2(data []byte) bool {
	// Quick scan — avoid full parse just to reject
	s := strings.TrimSpace(string(data))
	return strings.Contains(s, `"swagger"`) || strings.Contains(s, `swagger:`)
}

// HasQueryParams returns true if the operation has any query parameters.
func HasQueryParams(op Operation) bool {
	for _, p := range op.Parameters {
		if p.In == "query" {
			return true
		}
	}
	return false
}
