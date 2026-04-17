// Package mcp exposes every OpenAPI operation as an MCP tool.
// Supports stdio (for `claude mcp add`) and SSE (for remote deployments).
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/apimount/apimount/internal/core/exec"
	"github.com/apimount/apimount/internal/core/spec"
)

// Config holds MCP frontend configuration.
type Config struct {
	Transport string // "stdio" or "sse"
	Addr      string // listen address for SSE (default ":8080")
}

// Frontend is the MCP frontend. It converts each OpenAPI operation into an MCP
// tool, so Claude Code / Desktop / Cursor can call any API through apimount.
type Frontend struct {
	cfg      Config
	executor *exec.Executor
	spec     *spec.ParsedSpec
	baseURL  string
}

// New creates a new MCP frontend.
func New(cfg Config, ps *spec.ParsedSpec, executor *exec.Executor, baseURL string) *Frontend {
	if cfg.Transport == "" {
		cfg.Transport = "stdio"
	}
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	return &Frontend{cfg: cfg, executor: executor, spec: ps, baseURL: baseURL}
}

// Name implements frontend.Frontend.
func (f *Frontend) Name() string { return "mcp" }

// Serve builds the MCP server and starts the selected transport.
func (f *Frontend) Serve(ctx context.Context, _, _ any) error {
	srv := server.NewMCPServer(
		"apimount",
		f.spec.Version,
		server.WithToolCapabilities(true),
	)

	for i := range f.spec.Operations {
		op := &f.spec.Operations[i]
		tool := buildTool(op)
		handler := f.buildHandler(op)
		srv.AddTool(tool, handler)
	}

	switch f.cfg.Transport {
	case "sse":
		return f.serveSSE(ctx, srv)
	default:
		return f.serveStdio(ctx, srv)
	}
}

func (f *Frontend) serveStdio(_ context.Context, srv *server.MCPServer) error {
	stdio := server.NewStdioServer(srv)
	return stdio.Listen(context.Background(), os.Stdin, os.Stdout)
}

func (f *Frontend) serveSSE(ctx context.Context, srv *server.MCPServer) error {
	sse := server.NewSSEServer(srv)
	fmt.Fprintf(os.Stderr, "MCP SSE server listening on %s\n", f.cfg.Addr)
	httpSrv := &http.Server{Addr: f.cfg.Addr, Handler: sse}
	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()
	err := httpSrv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func buildTool(op *spec.Operation) mcp.Tool {
	name := op.OperationID
	if name == "" {
		name = strings.ToLower(op.Method) + "_" + sanitizePath(op.Path)
	}

	desc := op.Summary
	if desc == "" {
		desc = op.Description
	}
	desc = fmt.Sprintf("%s %s — %s", op.Method, op.Path, desc)

	schema := buildInputSchema(op)
	return mcp.NewToolWithRawSchema(name, desc, schema)
}

func (f *Frontend) buildHandler(op *spec.Operation) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()

		pathParams := make(map[string]string)
		queryParams := make(map[string]string)
		var body []byte

		for _, p := range op.Parameters {
			val, ok := args[p.Name]
			if !ok {
				continue
			}
			s := fmt.Sprintf("%v", val)
			switch p.In {
			case "path":
				pathParams[p.Name] = s
			case "query":
				queryParams[p.Name] = s
			}
		}

		if rawBody, ok := args["body"]; ok {
			switch v := rawBody.(type) {
			case string:
				body = []byte(v)
			default:
				body, _ = json.Marshal(v)
			}
		}

		var respBody []byte
		var err error

		switch strings.ToUpper(op.Method) {
		case "GET":
			respBody, err = f.executor.ExecuteGET(ctx, op, pathParams, queryParams)
		default:
			respBody, err = f.executor.ExecuteWrite(ctx, op, pathParams, queryParams, body)
		}

		if err != nil {
			errMsg := err.Error()
			if len(respBody) > 0 {
				errMsg += "\n" + string(respBody)
			}
			return mcp.NewToolResultError(errMsg), nil
		}

		return mcp.NewToolResultText(string(respBody)), nil
	}
}

func buildInputSchema(op *spec.Operation) json.RawMessage {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
	props := schema["properties"].(map[string]interface{})
	var required []string

	for _, p := range op.Parameters {
		prop := map[string]interface{}{
			"type":        schemaTypeOrString(p.Schema.Type),
			"description": paramDescription(p),
		}
		props[p.Name] = prop
		if p.Required {
			required = append(required, p.Name)
		}
	}

	if op.RequestBody != nil {
		props["body"] = map[string]interface{}{
			"description": "Request body (JSON)",
		}
		if op.RequestBody.Required {
			required = append(required, "body")
		}
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	data, _ := json.Marshal(schema)
	return data
}

func paramDescription(p spec.Parameter) string {
	desc := p.Description
	if desc == "" {
		desc = fmt.Sprintf("%s parameter '%s'", p.In, p.Name)
	}
	return desc
}

func schemaTypeOrString(t string) string {
	if t == "" {
		return "string"
	}
	return t
}

func sanitizePath(path string) string {
	r := strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_")
	s := r.Replace(strings.Trim(path, "/"))
	return strings.Trim(s, "_")
}
