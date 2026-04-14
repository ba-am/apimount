// Package plan builds and holds the virtual filesystem tree derived from a ParsedSpec.
package plan

import (
	"sync"

	"github.com/apimount/apimount/internal/core/spec"
)

// NodeType distinguishes directories from virtual files.
type NodeType int

const (
	NodeTypeDir  NodeType = iota
	NodeTypeFile
)

// FileRole defines what a file does when read or written.
type FileRole int

const (
	FileRoleGET      FileRole = iota // read → HTTP GET
	FileRolePost                      // write → HTTP POST
	FileRolePut                       // write → HTTP PUT
	FileRoleDelete                    // write → HTTP DELETE
	FileRolePatch                     // write → HTTP PATCH
	FileRoleSchema                    // read → JSON schema of request body (static)
	FileRoleQuery                     // write query params, read triggers GET with those params
	FileRoleHelp                      // read → human-readable description (static)
	FileRoleResponse                  // read → last response for this path (volatile)
)

// FSNode is one node in the virtual filesystem tree.
type FSNode struct {
	Name   string
	Type   NodeType
	Role   FileRole // only meaningful for NodeTypeFile
	Parent *FSNode

	// For dir nodes
	Children        map[string]*FSNode
	IsParamTemplate bool   // true if name is like "{petId}"
	ParamName       string // "petId" extracted from "{petId}"

	// For file nodes: the HTTP operation to execute
	Operation *spec.Operation

	// Static content (for .schema, .help files)
	StaticContent []byte

	// Runtime state — frontends copy these into per-open session objects;
	// the tree itself only stores the last-known values for warming .response reads.
	Mu           sync.RWMutex
	LastResponse []byte            // last response body
	QueryParams  map[string]string // accumulated query params

	// Path param bindings accumulated from parent dirs
	PathParams map[string]string
}

// NewDirNode creates a new directory node.
func NewDirNode(name string, parent *FSNode) *FSNode {
	return &FSNode{
		Name:       name,
		Type:       NodeTypeDir,
		Parent:     parent,
		Children:   make(map[string]*FSNode),
		PathParams: make(map[string]string),
	}
}

// NewFileNode creates a new file node.
func NewFileNode(name string, role FileRole, parent *FSNode, op *spec.Operation) *FSNode {
	return &FSNode{
		Name:       name,
		Type:       NodeTypeFile,
		Role:       role,
		Parent:     parent,
		Children:   make(map[string]*FSNode),
		Operation:  op,
		PathParams: make(map[string]string),
	}
}

// IsDir returns true if the node is a directory.
func (n *FSNode) IsDir() bool {
	return n.Type == NodeTypeDir
}
