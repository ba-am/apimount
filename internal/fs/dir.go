package fs

import (
	"context"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"go.uber.org/zap"

	"github.com/apimount/apimount/internal/tree"
)

// DirNode is a FUSE inode representing a directory in the virtual tree.
type DirNode struct {
	fs.Inode
	treeNode *tree.FSNode
	apifs    *APIFS
}

var _ fs.NodeLookuper = (*DirNode)(nil)
var _ fs.NodeReaddirer = (*DirNode)(nil)
var _ fs.NodeGetattrer = (*DirNode)(nil)

// Getattr returns directory attributes.
func (d *DirNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFDIR | 0555
	return fs.OK
}

// Lookup resolves a path component within this directory.
// For path-param template dirs, it dynamically creates virtual children.
func (d *DirNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	// Check static children first
	if child, ok := d.treeNode.Children[name]; ok {
		inode := d.buildInode(child, out)
		return inode, fs.OK
	}

	// Check if this dir has a path-param template child
	for _, child := range d.treeNode.Children {
		if child.IsParamTemplate {
			// name is a concrete value e.g. "1234" for {petId}
			bound := tree.CloneWithBinding(child, child.ParamName, name, d.treeNode)
			bound.Name = name

			d.apifs.logger.Debug("dynamic param lookup",
				zap.String("param", child.ParamName),
				zap.String("value", name),
			)

			inode := d.buildInode(bound, out)
			return inode, fs.OK
		}
	}

	return nil, syscall.ENOENT
}

// Readdir lists directory contents.
func (d *DirNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	entries := make([]fuse.DirEntry, 0, len(d.treeNode.Children))
	for name, child := range d.treeNode.Children {
		mode := uint32(syscall.S_IFREG | 0444)
		if child.Type == tree.NodeTypeDir {
			mode = syscall.S_IFDIR | 0555
		}
		entries = append(entries, fuse.DirEntry{
			Name: name,
			Mode: mode,
		})
	}
	return fs.NewListDirStream(entries), fs.OK
}

// buildInode creates a kernel inode for the given FSNode.
func (d *DirNode) buildInode(node *tree.FSNode, out *fuse.EntryOut) *fs.Inode {
	if node.Type == tree.NodeTypeDir {
		child := &DirNode{treeNode: node, apifs: d.apifs}
		out.Mode = syscall.S_IFDIR | 0555
		return d.NewInode(context.Background(), child, fs.StableAttr{
			Mode: syscall.S_IFDIR,
		})
	}

	// File node
	fn := &FileNode{treeNode: node, apifs: d.apifs}
	out.Mode = fileMode(node)
	return d.NewInode(context.Background(), fn, fs.StableAttr{
		Mode: syscall.S_IFREG,
	})
}

func fileMode(node *tree.FSNode) uint32 {
	switch node.Role {
	case tree.FileRolePost, tree.FileRolePut, tree.FileRoleDelete, tree.FileRolePatch, tree.FileRoleQuery:
		return syscall.S_IFREG | 0644 // read+write
	default:
		return syscall.S_IFREG | 0444 // read only
	}
}
