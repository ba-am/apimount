package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/apimount/apimount/internal/core/spec"
)

// ValidationConfig controls request-body validation behaviour.
type ValidationConfig struct {
	Enabled bool // default false; set via --validate flag
}

// ValidationError is returned when request body fails schema validation.
type ValidationError struct {
	OperationID string
	Violations  []Violation
}

// Violation describes a single schema mismatch.
type Violation struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func (e *ValidationError) Error() string {
	var parts []string
	for _, v := range e.Violations {
		parts = append(parts, fmt.Sprintf("%s: %s", v.Path, v.Reason))
	}
	return fmt.Sprintf("request body does not match schema for %s: %s", e.OperationID, strings.Join(parts, "; "))
}

// ValidateMiddleware validates request bodies against the operation's request
// schema before forwarding to the next handler. Only applies to operations that
// declare a request body with a JSON schema.
func ValidateMiddleware(cfg ValidationConfig) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request) (*Result, error) {
			if !cfg.Enabled {
				return next(ctx, req)
			}
			if req.Op == nil || req.Op.RequestBody == nil {
				return next(ctx, req)
			}
			if len(req.Body) == 0 {
				return next(ctx, req)
			}

			schema := &req.Op.RequestBody.Schema
			if schema.Type == "" && len(schema.Properties) == 0 {
				return next(ctx, req)
			}

			var body interface{}
			if err := json.Unmarshal(req.Body, &body); err != nil {
				return nil, &ValidationError{
					OperationID: req.Op.OperationID,
					Violations:  []Violation{{Path: "/", Reason: "invalid JSON: " + err.Error()}},
				}
			}

			violations := validateValue(body, schema, "")
			if len(violations) > 0 {
				return nil, &ValidationError{
					OperationID: req.Op.OperationID,
					Violations:  violations,
				}
			}

			return next(ctx, req)
		}
	}
}

func validateValue(val interface{}, schema *spec.Schema, path string) []Violation {
	if schema == nil {
		return nil
	}
	if path == "" {
		path = "/"
	}

	var violations []Violation

	if schema.Type != "" {
		if v := checkType(val, schema.Type, path); v != nil {
			return []Violation{*v}
		}
	}

	if len(schema.Enum) > 0 {
		if !enumContains(schema.Enum, val) {
			violations = append(violations, Violation{
				Path:   path,
				Reason: fmt.Sprintf("value not in enum %v", schema.Enum),
			})
		}
	}

	if schema.Type == "object" || (schema.Type == "" && len(schema.Properties) > 0) {
		obj, ok := val.(map[string]interface{})
		if !ok {
			return violations
		}

		for _, req := range schema.Required {
			if _, exists := obj[req]; !exists {
				violations = append(violations, Violation{
					Path:   joinPath(path, req),
					Reason: "required field missing",
				})
			}
		}

		for name, propSchema := range schema.Properties {
			propVal, exists := obj[name]
			if !exists {
				continue
			}
			ps := propSchema
			violations = append(violations, validateValue(propVal, &ps, joinPath(path, name))...)
		}
	}

	if (schema.Type == "array") && schema.Items != nil {
		arr, ok := val.([]interface{})
		if ok {
			for i, item := range arr {
				violations = append(violations, validateValue(item, schema.Items, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	}

	return violations
}

func checkType(val interface{}, expected string, path string) *Violation {
	actual := jsonType(val)
	switch expected {
	case "string":
		if actual != "string" {
			return &Violation{Path: path, Reason: fmt.Sprintf("expected string, got %s", actual)}
		}
	case "number", "integer":
		if actual != "number" {
			return &Violation{Path: path, Reason: fmt.Sprintf("expected %s, got %s", expected, actual)}
		}
	case "boolean":
		if actual != "boolean" {
			return &Violation{Path: path, Reason: fmt.Sprintf("expected boolean, got %s", actual)}
		}
	case "array":
		if actual != "array" {
			return &Violation{Path: path, Reason: fmt.Sprintf("expected array, got %s", actual)}
		}
	case "object":
		if actual != "object" {
			return &Violation{Path: path, Reason: fmt.Sprintf("expected object, got %s", actual)}
		}
	}
	return nil
}

func jsonType(val interface{}) string {
	switch val.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}

func enumContains(enum []interface{}, val interface{}) bool {
	for _, e := range enum {
		if fmt.Sprintf("%v", e) == fmt.Sprintf("%v", val) {
			return true
		}
	}
	return false
}

func joinPath(base, field string) string {
	if base == "/" {
		return "/" + field
	}
	return base + "/" + field
}
