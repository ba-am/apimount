package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/apimount/apimount/internal/core/plan"
	mcpFrontend "github.com/apimount/apimount/internal/frontend/mcp"
	nfsFrontend "github.com/apimount/apimount/internal/frontend/nfs"
	webdavFrontend "github.com/apimount/apimount/internal/frontend/webdav"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start a server frontend (mcp, webdav, nfs)",
	Long: `Start one of the non-CLI frontends. Each surface exposes the same API tree
backed by the same execution core.

  apimount serve mcp    --spec petstore.yaml   # MCP tools for Claude/agents
  apimount serve webdav --spec petstore.yaml   # Browse from Finder/Explorer
  apimount serve nfs    --spec petstore.yaml   # mount -t nfs 127.0.0.1:/`,
}

var serveMCPCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start an MCP tool server (stdio or SSE)",
	Long: `Expose every OpenAPI operation as an MCP tool.
Default transport is stdio (for claude mcp add). Use --transport sse for remote.`,
	RunE: runServeMCP,
}

var serveWebDAVCmd = &cobra.Command{
	Use:   "webdav",
	Short: "Start a WebDAV server",
	Long:  `Serve the API tree as a WebDAV filesystem. Connect from Finder, Explorer, or davfs2.`,
	RunE:  runServeWebDAV,
}

var serveNFSCmd = &cobra.Command{
	Use:   "nfs",
	Short: "Start an NFS server",
	Long:  `Serve the API tree as NFSv3. mount -t nfs -o vers=3,nolock 127.0.0.1:/ /mnt/api`,
	RunE:  runServeNFS,
}

func init() {
	serveMCPCmd.Flags().String("transport", "stdio", "MCP transport: stdio or sse")
	serveMCPCmd.Flags().String("addr", ":8080", "listen address for SSE transport")

	serveWebDAVCmd.Flags().String("addr", ":8080", "listen address")

	serveNFSCmd.Flags().String("addr", ":2049", "listen address")

	serveCmd.AddCommand(serveMCPCmd, serveWebDAVCmd, serveNFSCmd)
}

func runServeMCP(cmd *cobra.Command, _ []string) error {
	ls, err := loadSpecFromFlags()
	if err != nil {
		return err
	}
	executor, err := newExecutorFromFlags(ls)
	if err != nil {
		return err
	}

	transport, _ := cmd.Flags().GetString("transport")
	addr, _ := cmd.Flags().GetString("addr")

	frontend := mcpFrontend.New(
		mcpFrontend.Config{Transport: transport, Addr: addr},
		ls.ps, executor, ls.baseURL,
	)

	ctx := signalContext()
	return frontend.Serve(ctx, nil, nil)
}

func runServeWebDAV(cmd *cobra.Command, _ []string) error {
	ls, err := loadSpecFromFlags()
	if err != nil {
		return err
	}
	executor, err := newExecutorFromFlags(ls)
	if err != nil {
		return err
	}
	addr, _ := cmd.Flags().GetString("addr")
	root := plan.BuildTree(ls.ps, "path")

	frontend := webdavFrontend.New(
		webdavFrontend.Config{Addr: addr},
		root, executor, ls.baseURL,
	)

	ctx := signalContext()
	return frontend.Serve(ctx, nil, nil)
}

func runServeNFS(cmd *cobra.Command, _ []string) error {
	ls, err := loadSpecFromFlags()
	if err != nil {
		return err
	}
	executor, err := newExecutorFromFlags(ls)
	if err != nil {
		return err
	}
	addr, _ := cmd.Flags().GetString("addr")
	root := plan.BuildTree(ls.ps, "path")

	frontend := nfsFrontend.New(
		nfsFrontend.Config{Addr: addr},
		root, executor, ls.baseURL,
	)

	ctx := signalContext()
	return frontend.Serve(ctx, nil, nil)
}

func signalContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-ch
		fmt.Fprintf(os.Stderr, "\nReceived %s, shutting down...\n", sig)
		cancel()
	}()
	return ctx
}
