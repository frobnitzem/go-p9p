package ramfs

import (
	"context"
	"time"

	"github.com/frobnitzem/go-p9p"
)

// Rooted file hierarchy
type fServer struct {
	nextpath uint64
	root *FileEnt
}
func (fs *fServer) next() uint64 {
	fs.nextpath++
	return fs.nextpath
}

// User session connected to the file server.
type fSession struct {
	uname string
    umask uint32
	fs *fServer
}

// User pointer to a FileEnt (see inode.go)
type FileHandle struct { // implements p9p.Dirent
	Path string
	ent  *FileEnt
	sess *fSession
}

// Create all metadata for a new file / dir.
func (fs *fServer) newDir(fname string, isDir bool, uname string, mode uint32) p9p.Dir {
	path := fs.next()
	time := time.Now()
	dir := p9p.Dir{
		Qid: p9p.Qid{Path: path, Version: 0},
		Name: fname,
		Mode: mode,
		Length: 0,
		AccessTime: time,
		ModTime: time,
		UID: uname,
		GID: "users",
		MUID: uname,
	}

	if isDir {
		dir.Qid.Type |= p9p.QTDIR
		dir.Mode |= p9p.DMDIR
	}
	return dir
}

// Warning! Does not validate fname.  Do this before calling.
func (fs *fServer) Create(parent *FileEnt, info p9p.Dir) *FileEnt {
	f := &FileEnt{
			Info: info,
			fs: fs,
		}
	parent.link_child(f)
	return f
}

// Create a server that serves up a single "root" dir.
func NewServer(ctx context.Context) p9p.FileSys {
	return newServer()
}
func newServer() *fServer {
	fs := &fServer{}
	dir := fs.newDir("/", true, "root", 0775)
	root := &FileEnt{fs: fs, Info: dir}
	root.parents = []*FileEnt{root}
	fs.root = root
	return fs
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
	sess := fSession{
		uname: uname,
		umask: 0022,
		fs: fs,
	}
	return FileHandle{Path: "/", ent: fs.root, sess: &sess}, nil
}
