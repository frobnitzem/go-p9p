package ramfs

import (
	"context"
	"time"

	"github.com/frobnitzem/go-p9p"
)

// Rooted file hierarchy
type fServer struct {
	lastpath uint64
	root *FileEnt
}
func (fs *fServer) next() uint64 {
	fs.lastpath++
	return fs.lastpath
}
// global to all clients
var fserver fServer = fServer{
	lastpath: 1,
	root: &FileEnt{
		nref: 1,
		children: make(map[string]*FileEnt),
		//fs: &fserver,
		Info: newDir(1, "/", "root", p9p.DMDIR | 0775),
	},
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
	Mode p9p.Flag // due to defaults, 0 = OREAD
	ent  *FileEnt
	sess *fSession
	parents []*FileEnt // nonzero if this handle is not the root
}

// Create all metadata for a new file / dir.
func newDir(path uint64, fname string, uname string, mode uint32) p9p.Dir {
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

	if dir.Mode & p9p.DMDIR > 0 {
		dir.Qid.Type |= p9p.QTDIR
	}
	return dir
}

// Warning! Does not validate fname for things like "."
// Do this before calling.
// If successful, this returns a new FileEnt with one
// reference to it.
func (fs *fServer) Create(parent *FileEnt, info p9p.Dir) (*FileEnt, error) {
	f := &FileEnt{
			nref: 1,
			fs: fs,
			Info: info,
		}
	if info.Qid.Type&p9p.QTDIR != 0 {
		f.children = make(map[string]*FileEnt)
	}
	err := parent.link_child(info.Name, f)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Create a server that serves up a single "root" dir.
func NewServer(ctx context.Context) p9p.FileSys {
	fserver.root.fs = &fserver // init. cycle
	return &fserver
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
