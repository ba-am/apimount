package nfs

import (
	"context"
	"net"

	billy "github.com/go-git/go-billy/v5"
	gonfs "github.com/willscott/go-nfs"
)

type nfsHandler struct {
	fs *billyFS
}

func (h *nfsHandler) Mount(_ context.Context, _ net.Conn, _ gonfs.MountRequest) (gonfs.MountStatus, billy.Filesystem, []gonfs.AuthFlavor) {
	return gonfs.MountStatusOk, h.fs, []gonfs.AuthFlavor{gonfs.AuthFlavorNull}
}

func (h *nfsHandler) Change(_ billy.Filesystem) billy.Change {
	return nil
}

func (h *nfsHandler) FSStat(_ context.Context, _ billy.Filesystem, s *gonfs.FSStat) error {
	s.TotalSize = 1 << 30   // 1 GiB virtual
	s.FreeSize = 0
	s.AvailableSize = 0
	s.TotalFiles = 1000
	s.FreeFiles = 0
	s.AvailableFiles = 0
	s.CacheHint = 0
	return nil
}

func (h *nfsHandler) ToHandle(_ billy.Filesystem, path []string) []byte {
	return []byte("/" + joinPath(path))
}

func (h *nfsHandler) FromHandle(fh []byte) (billy.Filesystem, []string, error) {
	p := string(fh)
	if p == "/" || p == "" {
		return h.fs, nil, nil
	}
	parts := splitPath(p)
	return h.fs, parts, nil
}

func (h *nfsHandler) InvalidateHandle(_ billy.Filesystem, _ []byte) error {
	return nil
}

func (h *nfsHandler) HandleLimit() int {
	return 1024
}

func joinPath(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	s := ""
	for i, p := range parts {
		if i > 0 {
			s += "/"
		}
		s += p
	}
	return s
}

func splitPath(p string) []string {
	var out []string
	for _, s := range splitOn(p, '/') {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func splitOn(s string, sep byte) []string {
	var parts []string
	for {
		i := indexByte(s, sep)
		if i < 0 {
			parts = append(parts, s)
			break
		}
		parts = append(parts, s[:i])
		s = s[i+1:]
	}
	return parts
}

func indexByte(s string, c byte) int {
	for i := range len(s) {
		if s[i] == c {
			return i
		}
	}
	return -1
}
