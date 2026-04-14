package fuse

import (
	"context"
	"syscall"

	gofs "github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/apimount/apimount/internal/core/plan"
)

// DirNode is a FUSE inode representing a directory in the virtual tree.
type DirNode struct {
	gofs.Inode
	treeNode *plan.FSNode
	apifs    *APIFS
}

var _ gofs.NodeLookuper = (*DirNode)(nil)
var _ gofs.NodeReaddirer = (*DirNode)(nil)
var _ gofs.NodeGetattrer = (*DirNode)(nil)

// Getattr returns directory attributes.
func (d *DirNode) Getattr(_ context.Context, _ gofs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFDIR | 0555
	return gofs.OK
}

// Lookup resolves a path component within this directory.
func (d *DirNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*gofs.Inode, syscall.Errno) {
	if child, ok := d.treeNode.Children[name]; ok {
		return d.buildInode(child, out), gofs.OK
	}

	for _, child := range d.treeNode.Children {
		if child.IsParamTemplate {
			bound := plan.CloneWithBinding(child, child.ParamName, name, d.treeNode)
			bound.Name = name
			d.apifs.logger.Debug("dynamic param lookup",
				"param", child.ParamName,
				"value", name,
			)
			return d.buildInode(bound, out), gofs.OK
		}
	}

	return nil, syscall.ENOENT
}

// Readdir lists directory contents.
func (d *DirNode) Readdir(_ context.Context) (gofs.DirStream, syscall.Errno) {
	entries := make([]fuse.DirEntry, 0, len(d.treeNode.Children))
	for name, child := range d.treeNode.Children {
		mode := uint32(syscall.S_IFREG | 0444)
		if child.Type == plan.NodeTypeDir {
			mode = syscall.S_IFDIR | 0555
		}
		entries = append(entries, fuse.DirEntry{Name: name, Mode: mode})
	}
	return gofs.NewListDirStream(entries), gofs.OK
}

func (d *DirNode) buildInode(node *plan.FSNode, out *fuse.EntryOut) *gofs.Inode {
	if node.Type == plan.NodeTypeDir {
		child := &DirNode{treeNode: node, apifs: d.apifs}
		out.Mode = syscall.S_IFDIR | 0555
		return d.NewInode(context.Background(), child, gofs.StableAttr{Mode: syscall.S_IFDIR})
	}
	fn := &FileNode{treeNode: node, apifs: d.apifs}
	out.Mode = fileMode(node)
	return d.NewInode(context.Background(), fn, gofs.StableAttr{Mode: syscall.S_IFREG})
}

func fileMode(node *plan.FSNode) uint32 {
	switch node.Role {
	case plan.FileRolePost, plan.FileRolePut, plan.FileRoleDelete, plan.FileRolePatch, plan.FileRoleQuery:
		return syscall.S_IFREG | 0644
	default:
		return syscall.S_IFREG | 0444
	}
}
