package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/apimount/apimount/internal/core/spec"
)

// runHTTPCall is the shared implementation for `call`, `get`, `post`, `put`,
// `patch`, `delete`. It resolves the concrete URL path against the spec,
// executes the request via exec.Executor, and prints the response body.
func runHTTPCall(cmd *cobra.Command, method, concretePath string) error {
	ls, err := loadSpecFromFlags()
	if err != nil {
		return err
	}
	op, pathParams, err := spec.FindOperation(ls.ps, method, concretePath)
	if err != nil {
		return err
	}

	queryPairs, _ := cmd.Flags().GetStringSlice("query")
	headerPairs, _ := cmd.Flags().GetStringSlice("header")
	queryParams := parseKVList(queryPairs)
	_ = parseKVList(headerPairs) // header injection lands in Phase 4 middleware

	body, err := readBodyFromFlag(cmd)
	if err != nil {
		return err
	}

	executor, err := newExecutorFromFlags(ls)
	if err != nil {
		return err
	}

	ctx := context.Background()
	var respBody []byte
	var errno any

	switch strings.ToUpper(op.Method) {
	case "GET":
		respBody, errno, err = executor.ExecuteGET(ctx, op, pathParams, queryParams)
	default:
		respBody, errno, err = executor.ExecuteWrite(ctx, op, pathParams, queryParams, body)
	}
	_ = errno
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		if len(respBody) > 0 {
			writeJSONResponse(os.Stdout, respBody)
		}
		return fmt.Errorf("request failed")
	}
	writeJSONResponse(os.Stdout, respBody)
	return nil
}

var callCmd = &cobra.Command{
	Use:   "call METHOD PATH",
	Short: "Execute an HTTP request against the spec's base URL",
	Long: `Execute an arbitrary HTTP METHOD against PATH on the spec's base URL.
The path is matched against the spec's operations; path parameters are bound
from the concrete URL segments automatically.

Examples:
  apimount call GET  /pet/42 --spec petstore.yaml
  apimount call POST /pet --body '{"name":"Rex","photoUrls":[]}' --spec petstore.yaml
`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHTTPCall(cmd, args[0], args[1])
	},
}

var getCmd = &cobra.Command{
	Use:   "get PATH",
	Short: "HTTP GET against the spec",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runHTTPCall(cmd, "GET", args[0]) },
}

var postCmd = &cobra.Command{
	Use:   "post PATH",
	Short: "HTTP POST against the spec",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runHTTPCall(cmd, "POST", args[0]) },
}

var putCmd = &cobra.Command{
	Use:   "put PATH",
	Short: "HTTP PUT against the spec",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runHTTPCall(cmd, "PUT", args[0]) },
}

var patchCmd = &cobra.Command{
	Use:   "patch PATH",
	Short: "HTTP PATCH against the spec",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runHTTPCall(cmd, "PATCH", args[0]) },
}

var deleteCmd = &cobra.Command{
	Use:   "delete PATH",
	Short: "HTTP DELETE against the spec",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runHTTPCall(cmd, "DELETE", args[0]) },
}

func init() {
	for _, c := range []*cobra.Command{callCmd, getCmd, postCmd, putCmd, patchCmd, deleteCmd} {
		c.Flags().StringSlice("query", nil, "query parameter (key=value; repeatable)")
		c.Flags().StringSlice("header", nil, "custom header (key=value; repeatable) — Phase 4")
		c.Flags().String("body", "", "request body (inline string)")
		c.Flags().String("body-file", "", "request body from file (- for stdin)")
	}
}
