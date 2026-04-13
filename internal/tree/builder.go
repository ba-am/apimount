package tree

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/apimount/apimount/internal/spec"
)

// BuildTree builds an FSNode tree from a ParsedSpec.
// groupBy: "tags" | "path" | "flat"
func BuildTree(ps *spec.ParsedSpec, groupBy string) *FSNode {
	root := NewDirNode("/", nil)

	for i := range ps.Operations {
		op := &ps.Operations[i]

		switch groupBy {
		case "path":
			addOperationByPath(root, op)
		case "flat":
			addOperationFlat(root, op)
		default: // "tags"
			addOperationByTags(root, op)
		}
	}

	// Add .help files to every dir and populate .schema static content
	addHelpFilesRecursive(root, ps)
	populateSchemaContent(root)

	return root
}

// addOperationByTags groups operations under their first tag as top-level dir.
func addOperationByTags(root *FSNode, op *spec.Operation) {
	group := "untagged"
	if len(op.Tags) > 0 {
		group = op.Tags[0]
	}

	groupDir := getOrCreate(root, group, nil)

	segments := splitPath(op.Path)
	// Drop leading segment if it duplicates the tag group name
	if len(segments) > 0 && segments[0] == group {
		segments = segments[1:]
	}

	current := groupDir
	for _, seg := range segments {
		current = getOrCreate(current, seg, nil)
		if isPathParam(seg) && !current.IsParamTemplate {
			current.IsParamTemplate = true
			current.ParamName = extractParamName(seg)
		}
	}

	addOperationFiles(current, op)
}

// addOperationByPath groups operations by their path structure directly under root.
func addOperationByPath(root *FSNode, op *spec.Operation) {
	segments := splitPath(op.Path)
	current := root
	for _, seg := range segments {
		current = getOrCreate(current, seg, nil)
		if isPathParam(seg) && !current.IsParamTemplate {
			current.IsParamTemplate = true
			current.ParamName = extractParamName(seg)
		}
	}
	addOperationFiles(current, op)
}

// addOperationFlat places all operations in a flat directory named by operationId.
func addOperationFlat(root *FSNode, op *spec.Operation) {
	dir := getOrCreate(root, op.OperationID, nil)
	addOperationFiles(dir, op)
}

// addOperationFiles adds virtual operation files to a directory node.
func addOperationFiles(dir *FSNode, op *spec.Operation) {
	switch op.Method {
	case "GET":
		addFile(dir, ".data", FileRoleGET, op)
		if spec.HasQueryParams(*op) {
			addFile(dir, ".query", FileRoleQuery, op)
		}
	case "POST":
		addFile(dir, ".post", FileRolePost, op)
		if op.RequestBody != nil {
			addFile(dir, ".schema", FileRoleSchema, op)
		}
	case "PUT":
		addFile(dir, ".put", FileRolePut, op)
		if op.RequestBody != nil {
			addFile(dir, ".schema", FileRoleSchema, op)
		}
	case "DELETE":
		addFile(dir, ".delete", FileRoleDelete, op)
	case "PATCH":
		addFile(dir, ".patch", FileRolePatch, op)
		if op.RequestBody != nil {
			addFile(dir, ".schema", FileRoleSchema, op)
		}
	}
	addFile(dir, ".response", FileRoleResponse, op)
}

// addFile adds a file node to a directory, skipping duplicates.
func addFile(dir *FSNode, name string, role FileRole, op *spec.Operation) {
	if _, exists := dir.Children[name]; exists {
		return
	}
	dir.Children[name] = NewFileNode(name, role, dir, op)
}

// getOrCreate returns an existing child dir or creates a new one.
func getOrCreate(parent *FSNode, name string, pathParams map[string]string) *FSNode {
	if child, ok := parent.Children[name]; ok {
		return child
	}
	child := NewDirNode(name, parent)
	for k, v := range pathParams {
		child.PathParams[k] = v
	}
	parent.Children[name] = child
	return child
}

// addHelpFilesRecursive adds .help files to every directory.
func addHelpFilesRecursive(node *FSNode, ps *spec.ParsedSpec) {
	if node.Type != NodeTypeDir {
		return
	}
	helpNode := NewFileNode(".help", FileRoleHelp, node, nil)
	helpNode.StaticContent = []byte(generateHelpContent(node, ps))
	node.Children[".help"] = helpNode

	for _, child := range node.Children {
		if child.Type == NodeTypeDir {
			addHelpFilesRecursive(child, ps)
		}
	}
}

// generateHelpContent generates the text content for a .help file.
func generateHelpContent(dir *FSNode, ps *spec.ParsedSpec) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Directory: %s\n", dirPath(dir))
	sb.WriteString(strings.Repeat("━", 40) + "\n\n")

	// Operations at this level
	hasOps := false
	for name, child := range dir.Children {
		if child.Type != NodeTypeFile || child.Operation == nil {
			continue
		}
		if child.Role == FileRoleHelp || child.Role == FileRoleResponse || child.Role == FileRoleSchema {
			continue
		}
		if !hasOps {
			sb.WriteString("Operations:\n")
			hasOps = true
		}
		op := child.Operation
		desc := op.Summary
		if desc == "" {
			desc = op.Description
		}
		fmt.Fprintf(&sb, "  %-10s →  %s %s\n", name, op.Method, op.Path)
		if desc != "" {
			fmt.Fprintf(&sb, "             %s\n", desc)
		}
	}
	if !hasOps {
		sb.WriteString("No operations at this level. Browse subdirectories.\n")
	}

	// Files
	sb.WriteString("\nFiles in this directory:\n")
	for name, child := range dir.Children {
		if child.Type != NodeTypeFile {
			continue
		}
		fmt.Fprintf(&sb, "  %-12s → %s\n", name, fileRoleDescription(child.Role))
	}

	// Subdirs
	hasDirs := false
	for name, child := range dir.Children {
		if child.Type != NodeTypeDir {
			continue
		}
		if !hasDirs {
			sb.WriteString("\nSubdirectories:\n")
			hasDirs = true
		}
		if child.IsParamTemplate {
			fmt.Fprintf(&sb, "  %s/  (path parameter: %s)\n", name, child.ParamName)
		} else {
			fmt.Fprintf(&sb, "  %s/\n", name)
		}
	}

	fmt.Fprintf(&sb, "\nAPI: %s %s\n", ps.Title, ps.Version)
	if ps.BaseURL != "" {
		fmt.Fprintf(&sb, "Base URL: %s\n", ps.BaseURL)
	}

	return sb.String()
}

func fileRoleDescription(role FileRole) string {
	switch role {
	case FileRoleGET:
		return "read: execute HTTP GET, returns response body"
	case FileRolePost:
		return "write: execute HTTP POST with JSON body; read: last response"
	case FileRolePut:
		return "write: execute HTTP PUT with JSON body; read: last response"
	case FileRoleDelete:
		return "write: execute HTTP DELETE; read: last response"
	case FileRolePatch:
		return "write: execute HTTP PATCH with JSON body; read: last response"
	case FileRoleSchema:
		return "read: JSON schema of request body"
	case FileRoleQuery:
		return "write: key=val&key2=val2 query params; read: GET with those params"
	case FileRoleHelp:
		return "this file"
	case FileRoleResponse:
		return "read: last raw response (status + headers + body)"
	}
	return "unknown"
}

// dirPath returns a human-readable path for a directory node.
func dirPath(node *FSNode) string {
	var parts []string
	for cur := node; cur != nil && cur.Name != "/"; cur = cur.Parent {
		parts = append([]string{cur.Name}, parts...)
	}
	if len(parts) == 0 {
		return "/"
	}
	return "/" + strings.Join(parts, "/")
}

// splitPath splits an OpenAPI path into segments, removing empty parts.
func splitPath(path string) []string {
	out := []string{}
	for _, p := range strings.Split(strings.Trim(path, "/"), "/") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isPathParam returns true if the segment is a path parameter like {petId}.
func isPathParam(seg string) bool {
	return strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")
}

// extractParamName extracts "petId" from "{petId}".
func extractParamName(seg string) string {
	return strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}")
}

// CloneWithBinding clones a subtree binding a path param to a concrete value.
// All descendant file nodes inherit the binding in their PathParams.
func CloneWithBinding(template *FSNode, paramName, paramValue string, parent *FSNode) *FSNode {
	cloned := NewDirNode(paramValue, parent)

	// Inherit path params from parent chain, then add the new binding
	for k, v := range parent.PathParams {
		cloned.PathParams[k] = v
	}
	cloned.PathParams[paramName] = paramValue

	for name, child := range template.Children {
		switch child.Type {
		case NodeTypeFile:
			fc := NewFileNode(name, child.Role, cloned, child.Operation)
			fc.StaticContent = child.StaticContent
			for k, v := range cloned.PathParams {
				fc.PathParams[k] = v
			}
			cloned.Children[name] = fc
		case NodeTypeDir:
			dc := CloneWithBinding(child, paramName, paramValue, cloned)
			dc.Name = child.Name
			dc.IsParamTemplate = child.IsParamTemplate
			dc.ParamName = child.ParamName
			cloned.Children[name] = dc
		}
	}

	return cloned
}

// populateSchemaContent walks the tree and sets StaticContent on all .schema nodes.
func populateSchemaContent(node *FSNode) {
	if node.Type == NodeTypeFile && node.Role == FileRoleSchema {
		node.StaticContent = generateSchemaJSON(node.Operation)
		return
	}
	for _, child := range node.Children {
		populateSchemaContent(child)
	}
}

// generateSchemaJSON produces a pretty-printed JSON schema from an operation's request body.
func generateSchemaJSON(op *spec.Operation) []byte {
	if op == nil || op.RequestBody == nil {
		return []byte("{}\n")
	}
	rb := op.RequestBody

	type propOut struct {
		Type        string      `json:"type,omitempty"`
		Format      string      `json:"format,omitempty"`
		Description string      `json:"description,omitempty"`
		Example     any         `json:"example,omitempty"`
		Enum        []any       `json:"enum,omitempty"`
		Items       *propOut    `json:"items,omitempty"`
	}
	type schemaOut struct {
		Type        string              `json:"type,omitempty"`
		Required    []string            `json:"required,omitempty"`
		Properties  map[string]*propOut `json:"properties,omitempty"`
		Description string              `json:"description,omitempty"`
	}

	var buildProp func(s spec.Schema) *propOut
	buildProp = func(s spec.Schema) *propOut {
		p := &propOut{
			Type:        s.Type,
			Format:      s.Format,
			Description: s.Description,
			Example:     s.Example,
			Enum:        s.Enum,
		}
		if s.Items != nil {
			p.Items = buildProp(*s.Items)
		}
		return p
	}

	out := schemaOut{
		Type:        rb.Schema.Type,
		Required:    rb.Schema.Required,
		Description: rb.Description,
	}
	if len(rb.Schema.Properties) > 0 {
		out.Properties = make(map[string]*propOut)
		for k, v := range rb.Schema.Properties {
			out.Properties[k] = buildProp(v)
		}
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Appendf(nil, "{\"error\": %q}\n", err.Error())
	}
	return append(data, '\n')
}

func printNode(node *FSNode, indent string) string {
	var sb strings.Builder
	if node.Type == NodeTypeDir {
		if node.Name == "/" {
			// Root: skip the "/" line, print children directly
			for _, child := range sortedChildren(node) {
				sb.WriteString(printNode(child, indent))
			}
			return sb.String()
		}
		fmt.Fprintf(&sb, "%s%s/\n", indent, node.Name)
		for _, child := range sortedChildren(node) {
			sb.WriteString(printNode(child, indent+"  "))
		}
	} else {
		fmt.Fprintf(&sb, "%s%s\n", indent, node.Name)
	}
	return sb.String()
}

// sortedChildren returns children sorted: dirs first, then files, each group alphabetical.
func sortedChildren(node *FSNode) []*FSNode {
	dirs := make([]*FSNode, 0, len(node.Children))
	files := make([]*FSNode, 0, len(node.Children))
	for _, child := range node.Children {
		if child.Type == NodeTypeDir {
			dirs = append(dirs, child)
		} else {
			files = append(files, child)
		}
	}
	sortNodes(dirs)
	sortNodes(files)
	return append(dirs, files...)
}

func sortNodes(nodes []*FSNode) {
	for i := 1; i < len(nodes); i++ {
		for j := i; j > 0 && nodes[j].Name < nodes[j-1].Name; j-- {
			nodes[j], nodes[j-1] = nodes[j-1], nodes[j]
		}
	}
}

// PrintTree returns a string representation of the full tree rooted at root.
func PrintTree(root *FSNode) string {
	return printNode(root, "")
}
