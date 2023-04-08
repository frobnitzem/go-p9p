package ufs

import (
	"os"
	"context"
	"path/filepath"

	"github.com/frobnitzem/go-p9p"
)

type fServer struct {
    Base    string
    rootRef FileRef
}

// invariants:
//   Path always begins with '/'
//   Path never contains . or .. components
type FileRef struct {
    fs      *fServer
	Path    string // relative to root
	Info    p9p.Dir
}

func (ref *FileRef) fullPath() string {
    return filepath.Join(ref.fs.Base, ref.Path)
}

// Find the absolute path of name relative to dir.
// dir must be an absolute path.
func relName(dir string, name string) string {
    if !filepath.IsAbs(name) { // first make absolute
        name = filepath.Join(dir, name)
    }
    return filepath.Clean(name) // normalize and remove leading ../
}

func (fs *fServer) newRef(rel string) (*FileRef, error) {
    if len(rel) < 1 || !filepath.IsAbs(rel) {
        return nil, p9p.MessageRerror{Ename: "Invalid path"}
    }
    // Normalizes, and removes any leading ../
    rel = filepath.Clean(rel)

	info, err := os.Stat(filepath.Join(fs.Base, rel))
	if err != nil {
		return nil, err
	}

    return &FileRef{fs: fs, Path: rel, Info: dirFromInfo(info)}, nil
}


func NewServer(ctx context.Context, root string) p9p.FServer {
    return &fServer{
        Base:    filepath.Clean(root),
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
