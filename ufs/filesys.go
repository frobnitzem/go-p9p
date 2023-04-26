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

// Internal path invariants:
//
//   - Path always begins with "/".
//   - Path does not contain any "\\" characters.
//   - Path never contains "." or ".." or "" (empty) elements.
type FileRef struct {
	fs   *fServer
	file *os.File
	SFid *p9p.SFid
	Path string
	Info p9p.Dir
}

// These fullPath functions should be the only way used to create a path
// referencing the underlying system.  They ensure
// we only access files inside our domain.

// Return the system's underlying path for the internal path, p.
//
// Assumes fs.Base is a valid full-path on the
// host filesystem.  Validates the argument, p.
func (fs *fServer) fullPath(p string) (string, error) {
	if !path.IsAbs(p) || strings.Contains(p, "\\") {
		return "", p9p.MessageRerror{Ename: "Invalid path"}
	}
	if path.Clean(p) != p { // removes ../ at root.
		return "", p9p.MessageRerror{Ename: "Invalid path"}
	}
	return filepath.Join(fs.Base, filepath.FromSlash(p)), nil
}

// Return the system's underlying path for the ref.
// Assumes fs.Base is a valid full-path on the
// host filesystem.  Also assumes ref.Path is
// a valid internal path.
func (ref FileRef) fullPath() string {
	return filepath.Join(ref.fs.Base, filepath.FromSlash(ref.Path))
}

// Create a new FileRef pointing to absolute path, p
// p must be a valid internal path, or else an
// error is returned (checked by fullPath function).
func (fs *fServer) newRef(p string) (*FileRef, error) {
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

func NewServer(ctx context.Context, root string) p9p.FileSys {
	return &fServer{
		Base: filepath.Clean(root),
	}
}

func (_ *fServer) RequireAuth(_ context.Context) bool {
	return false
}
func (fs *fServer) Auth(ctx context.Context,
	uname, aname string) (p9p.AuthFile, error) {
	return nil, nil
}
func (fs *fServer) Attach(ctx context.Context, uname, aname string,
	af p9p.AuthFile) (p9p.Dirent, error) {
	return fs.newRef("/")
}
