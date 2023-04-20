package ramfs

import (
	"context"
	"io"

	p9p "github.com/frobnitzem/go-p9p"
)

var ENotImpl error = p9p.MessageRerror{Ename: "not implemented"} 
var noHandle FileHandle = FileHandle{Path:"/", ent:nil, sess:nil}

func (f *FileEnt) IsDir() bool {
	return f.Info.Mode&p9p.DMDIR > 0
}

func (ref *FileEnt) Qid() p9p.Qid {
	return ref.Info.Qid
}
func (h FileHandle) Qid() p9p.Qid {
	return h.ent.Qid()
}

type dirList struct {
	dirs []p9p.Dir
	done bool
}

func (d *dirList) Next(ctx context.Context) ([]p9p.Dir, error) {
	if d.done {
		return nil, nil
	}
	d.done = true
	return d.dirs, nil
}

func (ref *FileEnt) OpenDir(ctx context.Context) (p9p.ReadNext, error) {
	if !ref.IsDir() {
		return nil, p9p.MessageRerror{Ename: "not a directory"}
	}

	var files []*FileEnt
	var dirs []p9p.Dir
	for _, file := range files {
		dirs = append(dirs, file.Info)
	}
	return (&dirList{dirs, false}).Next, nil
}
func (h FileHandle) OpenDir(ctx context.Context) (p9p.ReadNext, error) {
	return h.ent.OpenDir(ctx)
}

func (_ FileHandle) SetInfo(sfid *p9p.SFid) { }

func (_ FileHandle) Clunk(ctx context.Context) error {
	return nil
}

func (h FileHandle) Remove(ctx context.Context) error {
	h.Clunk(ctx)
	if h.ent.fs.root == h.ent {
		return p9p.MessageRerror{Ename: "cannot remove root"}
	}
	// TODO(frobnitzem): permission check
	return h.ent.Remove()
}

func (ref *FileEnt) Walk(ctx context.Context, names ...string) ([]p9p.Qid, *FileEnt, error) {
	if len(names) == 0 { // Clone
		return nil, ref, nil
	}

	// TODO(frobnitzem): return qid-s corresponding to partial walk
	/*if err != nil {
		return nil, nil, err
	}
	qids := make([]p9p.Qid, len(names))
	qids[len(qids)-1] = next.Qid()
	return qids, next, err*/
	return nil, nil, ENotImpl
}
func (h FileHandle) Walk(ctx context.Context, names ...string) ([]p9p.Qid, p9p.Dirent, error) {
	newpath, err := p9p.WalkName(h.Path, names...)
	if err != nil {
		return nil, noHandle, err
	}

	qids, ref, err := h.ent.Walk(ctx, names...)
	if err != nil {
		return qids, noHandle, err
	}
	return qids, FileHandle{ent: ref, Path: newpath, sess:h.sess}, nil
}

func (h FileHandle) Create(ctx context.Context, name string,
	mode uint32, perm p9p.Flag) (p9p.Dirent, p9p.File, error) {
	// TODO(frobnitzem): permission check
	h2, err := h.createImpl(name, mode&p9p.DMDIR > 0)
	if err != nil {
		return noHandle, noHandle, err
	}
	return h2, h2, nil
}
func (h FileHandle) createImpl(fname string, isDir bool) (FileHandle, error) {
	path, err := p9p.CreateName(h.Path, fname)
	if err != nil {
		return noHandle, err
	}
	sess := h.sess
	dir := sess.fs.newDir(fname, isDir, sess.uname, uint32(0777) & ^sess.umask)
	ent := sess.fs.Create(h.ent, dir)
	return FileHandle{Path: path, ent: ent, sess: sess}, nil
}

func (ref *FileEnt) Stat(ctx context.Context) (p9p.Dir, error) {
	return ref.Info, nil
}
func (h FileHandle) Stat(ctx context.Context) (p9p.Dir, error) {
	return h.ent.Stat(ctx)
}

func (ref *FileEnt) WStat(ctx context.Context, dir p9p.Dir) error {
	return ENotImpl
}
func (h FileHandle) WStat(ctx context.Context, dir p9p.Dir) error {
	return h.ent.WStat(ctx, dir)
}

func (h FileHandle) Open(ctx context.Context, mode p9p.Flag) (p9p.File, error) {
	// TODO(frobnitzem): permission check
	return h, nil
}

func (ref *FileEnt) Read(ctx context.Context, p []byte,
	offset int64) (int, error) {
	ref.Lock()
	defer ref.Unlock()

	n := int64(len(ref.Data))
	if offset >= n {
		return 0, io.EOF
	}
	m := int64(len(p))
	if offset+m > n {
		m = n - offset
	}
	copy(p[:m], ref.Data[offset:offset+m])
	return int(m), nil
}
func (h FileHandle) Read(ctx context.Context, p []byte,
    offset int64) (n int, err error) {
	return h.ent.Read(ctx, p, offset)
}

func (ref *FileEnt) Write(ctx context.Context, p []byte,
	offset int64) (int, error) {
	ref.Lock()
	defer ref.Unlock()

	n := int64(len(ref.Data))
	if offset > n {
		return 0, p9p.MessageRerror{Ename: "invalid address"}
	}
	ref.Info.Qid.Version++

	m := int64(len(p))
	if offset+m > n {
		n = n-offset // in [0:m)
		if n > 0 {
			copy(ref.Data[offset:offset+n], p[:n])
		}
		ref.Data = append(ref.Data, p[n:]...)
	} else {
		copy(ref.Data[offset:offset+m], p)
	}
	return int(m), nil
}
func (h FileHandle) Write(ctx context.Context, p []byte,
    offset int64) (n int, err error) {
	return h.ent.Write(ctx, p, offset)
}

func (_ FileHandle) IOUnit() int {
	return 0
}
