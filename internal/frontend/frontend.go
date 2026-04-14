// Package frontend defines the Frontend interface that all surface implementations must satisfy.
// Each frontend (FUSE, NFS, WebDAV, MCP, CLI) receives the same plan.Node tree and exec.Executor
// and translates user interactions into core/exec.Request calls.
//
// Hard layering rule: nothing under internal/frontend/* may import another frontend package.
// All business logic lives in internal/core/exec; frontends are thin adapters.
package frontend

import (
	"context"
)

// Frontend is the interface implemented by every surface layer.
// Serve blocks until ctx is cancelled.
type Frontend interface {
	// Name returns a short identifier, e.g. "fuse", "nfs", "webdav", "mcp", "cli".
	Name() string

	// Serve runs the frontend. It must return when ctx is done.
	// plan and exec are passed as interface{} here to avoid circular imports at the
	// interface definition layer; each concrete frontend casts them to the concrete types.
	Serve(ctx context.Context, plan any, exec any) error
}
