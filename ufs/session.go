package ufs

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/frobnitzem/go-p9p"
)

type fServer struct {
	Base    string // Base path of file server, in OS format.
	rootRef FileRef
}

// invariants:
//
//	Path always begins with '/'
//	Path never contains . or .. components
type FileRef struct {
	fs   *fServer
	Path string // Unix-separated path, relative to root
	Info p9p.Dir
}

// These fullPath functions should be the only way used to create a path
// referencing the underlying system.  They ensure
// we only access files inside our domain.

// Return the system's underlying path for the
// absolute path, p (within the server's domain).
func (fs *fServer) fullPath(p string) (string, error) {
	if strings.Contains(p, "\\") {
		return "", p9p.MessageRerror{Ename: "Invalid path"}
	}
	rel := path.Clean(p) // removes ../ at root.
	return filepath.Join(fs.Base, filepath.FromSlash(rel)), nil
}

// Return the system's underlying path for the ref.
func (ref *FileRef) fullPath() string {
	return filepath.Join(ref.fs.Base, filepath.FromSlash(ref.Path))
}

// Find the absolute path of names relative to dir.
// dir must be an absolute (Unix-convention) path.
// names cannot contain filepath separators (checked by p9p.ValidPath).
func relName(dir string, names ...string) (string, error) {
	depth := strings.Count(dir, "/") - 1
	bsp := p9p.ValidPath(names)
	if bsp > depth {
		return dir, p9p.MessageRerror{Ename: "Invalid path"}
	}

	return path.Join(dir, path.Join(names...)), nil
}

// Create a new *FileRef pointing to absolute path, p
func (fs *fServer) newRef(p string) (*FileRef, error) {
	if !path.IsAbs(p) {
		return nil, p9p.MessageRerror{Ename: "Invalid path"}
	}
	// Normalizes, and removes any leading ../
	p = path.Clean(p)
	fpath, err := fs.fullPath(p)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(fpath)
	if err != nil {
		return nil, err
	}

	return &FileRef{fs: fs, Path: p, Info: dirFromInfo(info)}, nil
}

func NewServer(ctx context.Context, root string) p9p.FServer {
	return &fServer{
		Base: filepath.Clean(root),
	}
}

func (fs *fServer) RequireAuth() bool {
	return false
}
func (fs *fServer) Auth(ctx context.Context, uname, aname string) p9p.AuthFile {
	return nil
}
func (fs *fServer) Root(ctx context.Context, uname, aname string,
	af p9p.AuthFile) (p9p.Dirent, error) {
	return fs.newRef("/")
}
