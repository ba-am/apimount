// Package spec loads and parses OpenAPI 3.0/3.1 documents into the internal ParsedSpec
// model using libopenapi as the underlying parser.
package spec

import (
	"fmt"
	"strings"

	"github.com/pb33f/libopenapi"
	highbase "github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
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

// Schema is a minimal JSON Schema subset used for rendering .schema files.
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

// Parse loads raw spec bytes and returns a ParsedSpec using libopenapi.
func Parse(data []byte, _ string) (*ParsedSpec, error) {
	if isSwagger2(data) {
		return nil, fmt.Errorf("unsupported spec version: 2.0 (Swagger). Only OpenAPI 3.x is supported")
	}

	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, fmt.Errorf("invalid OpenAPI spec: %w", err)
	}

	model, err := doc.BuildV3Model()
	if err != nil && model == nil {
		return nil, fmt.Errorf("could not build OpenAPI model: %w", err)
	}

	ps := &ParsedSpec{}
	d := &model.Model

	if d.Info != nil {
		ps.Title = d.Info.Title
		ps.Version = d.Info.Version
	}

	if len(d.Servers) > 0 {
		ps.BaseURL = resolveServerURL(d.Servers[0])
	}

	if d.Components != nil && d.Components.SecuritySchemes != nil {
		for name, scheme := range d.Components.SecuritySchemes.FromOldest() {
			ps.AuthSchemes = append(ps.AuthSchemes, parseAuthScheme(name, scheme))
		}
	}

	if d.Paths != nil && d.Paths.PathItems != nil {
		for path, item := range d.Paths.PathItems.FromOldest() {
			ops := item.GetOperations()
			if ops == nil {
				continue
			}
			for method, op := range ops.FromOldest() {
				parsed := parseOperation(strings.ToUpper(method), path, op)
				ps.Operations = append(ps.Operations, parsed)
			}
		}
	}

	return ps, nil
}

func resolveServerURL(s *v3high.Server) string {
	if s == nil {
		return ""
	}
	u := s.URL
	if s.Variables != nil {
		for k, v := range s.Variables.FromOldest() {
			if v.Default != "" {
				u = strings.ReplaceAll(u, "{"+k+"}", v.Default)
			}
		}
	}
	return strings.TrimRight(u, "/")
}

func parseOperation(method, path string, op *v3high.Operation) Operation {
	if op == nil {
		return Operation{Method: method, Path: path}
	}
	out := Operation{
		Method:      method,
		Path:        path,
		Summary:     op.Summary,
		Description: op.Description,
		Tags:        op.Tags,
		Responses:   make(map[int]Response),
	}

	if op.OperationId != "" {
		out.OperationID = op.OperationId
	} else {
		out.OperationID = generateOperationID(method, path)
	}

	for _, p := range op.Parameters {
		if p == nil {
			continue
		}
		param := Parameter{
			Name:        p.Name,
			In:          p.In,
			Description: p.Description,
		}
		if p.Required != nil {
			param.Required = *p.Required
		}
		if p.Schema != nil {
			if s := p.Schema.Schema(); s != nil {
				param.Schema = parseSchema(s)
			}
		}
		out.Parameters = append(out.Parameters, param)
	}

	if op.RequestBody != nil {
		rb := op.RequestBody
		reqBodyRequired := false
		if rb.Required != nil {
			reqBodyRequired = *rb.Required
		}
		reqBody := &RequestBody{
			Required:    reqBodyRequired,
			Description: rb.Description,
		}
		if rb.Content != nil {
			for ct, mt := range rb.Content.FromOldest() {
				reqBody.ContentType = ct
				if mt.Schema != nil {
					if s := mt.Schema.Schema(); s != nil {
						reqBody.Schema = parseSchema(s)
					}
				}
				break
			}
		}
		out.RequestBody = reqBody
	}

	if op.Responses != nil && op.Responses.Codes != nil {
		for code, r := range op.Responses.Codes.FromOldest() {
			if r == nil {
				continue
			}
			resp := Response{Description: r.Description}
			if r.Content != nil {
				for ct, mt := range r.Content.FromOldest() {
					resp.ContentType = ct
					if mt.Schema != nil {
						if s := mt.Schema.Schema(); s != nil {
							resp.Schema = parseSchema(s)
						}
					}
					break
				}
			}
			if code := parseStatusCode(code); code > 0 {
				out.Responses[code] = resp
			}
		}
	}

	for _, sr := range op.Security {
		if sr == nil || sr.Requirements == nil {
			continue
		}
		req := SecurityReq{}
		for name, scopes := range sr.Requirements.FromOldest() {
			req[name] = scopes
		}
		out.Security = append(out.Security, req)
	}

	return out
}

func parseSchema(s *highbase.Schema) Schema {
	if s == nil {
		return Schema{}
	}

	schemaType := ""
	if len(s.Type) > 0 {
		schemaType = s.Type[0]
	}

	out := Schema{
		Type:        schemaType,
		Format:      s.Format,
		Description: s.Description,
		Required:    s.Required,
	}

	// Example
	if s.Example != nil {
		out.Example = s.Example.Value
	}

	// Enum — convert yaml.Node values to interface{}
	for _, node := range s.Enum {
		if node != nil {
			out.Enum = append(out.Enum, node.Value)
		}
	}

	// Properties
	if s.Properties != nil {
		out.Properties = make(map[string]Schema)
		for k, proxy := range s.Properties.FromOldest() {
			if proxy != nil {
				if child := proxy.Schema(); child != nil {
					out.Properties[k] = parseSchema(child)
				}
			}
		}
	}

	// Items — OpenAPI 3.1 Items is DynamicValue[*SchemaProxy, bool]
	if s.Items != nil && s.Items.IsA() && s.Items.A != nil {
		if child := s.Items.A.Schema(); child != nil {
			items := parseSchema(child)
			out.Items = &items
		}
	}

	return out
}

func parseAuthScheme(name string, s *v3high.SecurityScheme) AuthScheme {
	if s == nil {
		return AuthScheme{Name: name}
	}
	as := AuthScheme{
		Name:   name,
		Type:   s.Type,
		Scheme: strings.ToLower(s.Scheme),
		In:     s.In,
		Param:  s.Name,
	}
	return as
}

func generateOperationID(method, path string) string {
	seg := strings.ToLower(method) + "_" + strings.ToLower(path)
	seg = strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(seg)
	seg = strings.Trim(seg, "_")
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
