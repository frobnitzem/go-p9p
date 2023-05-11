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

func (ref *FileEnt) OpenDir(ctx context.Context,
							dotdot p9p.Dir) (p9p.ReadNext, error) {
	ref.Lock()
	defer ref.Unlock()
	if !ref.IsDir() {
		return nil, p9p.MessageRerror{Ename: "not a directory"}
	}

	dirs := []p9p.Dir{dotdot}
	for _, file := range ref.children {
		dirs = append(dirs, file.Info)
	}
	return (&dirList{dirs, false}).Next, nil
}
func (h FileHandle) OpenDir(ctx context.Context) (p9p.ReadNext, error) {
	var dotdot p9p.Dir
	if len(h.parents) == 0 {
		dotdot = withName("..", h.ent.Info)
	} else {
		dotdot = withName("..", h.parents[len(h.parents)-1].Info)
	}

	return h.ent.OpenDir(ctx, dotdot)
}

func (h FileHandle) Clunk(ctx context.Context) error {
	h.ent.decref()
	for i := range h.parents {
		h.parents[len(h.parents)-i-1].decref()
	}
	return nil
}

// This remove command undoes the parent -> child link that
// was traversed to arrive at h.
// If some other process has already removed this link, then
// remove does nothing.
func (h FileHandle) Remove(ctx context.Context) error {
	defer h.Clunk(ctx)

	if len(h.parents) == 0 {
		return p9p.MessageRerror{Ename: "cannot remove root"}
	}
	// TODO(frobnitzem): permission check

	p := h.parents[len(h.parents)-1]
	// TODO(frobnitzem): consider using h.Name here?
	err := p.unlink_child(h.ent.Info.Name)
	if err == nil { // remove parent -> child ref count
		h.ent.decref()
	}

	return err
}

// Called after verifying names create a valid path expression.
// and does not contain ..
func (ref *FileEnt) Walk(names ...string) []*FileEnt {
	ans := make([]*FileEnt, len(names))
	var i int

	for i = 0; i < len(names); i++ {
		var found bool
		ref, found = ref.children[names[i]]
		if !found {
			break
		}
		ans[i] = ref
	}
	return ans[:i]
}

func (h FileHandle) Walk(ctx context.Context, names ...string) ([]p9p.Qid, p9p.Dirent, error) {
	// TODO(frobnitzem): permission check
	var qids []p9p.Qid
	newpath, err := p9p.WalkName(h.Path, names...)
	if err != nil {
		return nil, noHandle, err
	}

	var ndel int
	for ndel=0; ndel<len(names); ndel++ {
		if names[ndel] != ".." {
			break
		}
	}
	if ndel > len(h.parents) {
		return nil, noHandle, p9p.MessageRerror{Ename: "invalid path"}
	}

	// walk backward
	ans := make([]*FileEnt, ndel)
	ref := h.ent
	if ndel > 0 {
		ref = h.parents[len(h.parents)-ndel]
	}
	for i:=0; i<ndel; i++ {
		ans[i] = h.parents[len(h.parents)-1-i]
	}

	// walk forward
	ans = append(ans, ref.Walk(names[ndel:]...)...)

	sz := len(ans)
	success := true
	if len(names) > 0 {
		if sz != len(names) {
			success = false
		}
		if sz == 0 { // first step unsuccessful
			return nil, noHandle, p9p.ErrNotfound
		}
	}

	rh := FileHandle{
		Path: newpath,
		sess: h.sess,
	}

	// If walk was successful, increment file ref counts.
	if success {
		// cut ndel elements from parents and beginning of ans
		// e.g. for names = ["..", "..", "x"], and h.ref = /a/b/c
		// then ndel = 2
		// ans = [/a/b, /a, /a/x], sz = 3
		// h.parents = [/, /a, /a/b]
		// ref = /a
		// rh.ent = /a/x
		// rh.parents = [/, /a]
		//
		// for names = [".."] and h.ref = /x
		// then ndel = 1
		// ans = [/], sz = 1
		// h.parents = [/]
		// ref = /
		// rh.ent = /
		// rh.parents = []
		rh.parents = make([]*FileEnt, len(h.parents)-ndel + 1 + len(ans)-ndel)

		// would use append, but append mutates its input
		/*rh.parents = append(append(
							h.parents[:len(h.parents)-ndel],
							ref),
							ans[ndel:]...)*/
		i0 := len(h.parents)-ndel + 1
		for i := range rh.parents {
			var p *FileEnt
			if i < len(h.parents)-ndel {
				p = h.parents[i]
			} else if i >= i0 {
				p = ans[ndel+i-i0]
			} else {
				p = ref
			}
			p.incref()
			rh.parents[i] = p
		}
		rh.ent = rh.parents[len(rh.parents)-1]
		rh.parents = rh.parents[:len(rh.parents)-1]
	} else {
		rh = noHandle
	}

	qids = make([]p9p.Qid, len(ans))
	for i, a := range ans {
		qids[i] = a.Info.Qid
	}

	return qids, rh, nil
}

func (h FileHandle) Create(ctx context.Context, name string,
	perm uint32, mode p9p.Flag) (p9p.Dirent, p9p.File, error) {
	// TODO(frobnitzem): permission check
	h2, err := h.createImpl(name, perm)
	if err != nil {
		return noHandle, noHandle, err
	}
	h2.Mode = mode
	return h2, h2, nil
}

func (sess *fSession) newDir(fname string, mode uint32) p9p.Dir {
	mode = mode ^ (mode & sess.umask) // turn off bits matching the umask
	return newDir(sess.fs.next(), fname, sess.uname, mode)
}

func (h FileHandle) createImpl(fname string, mode uint32) (FileHandle, error) {
	path, err := p9p.CreateName(h.Path, fname)
	if err != nil {
		return noHandle, err
	}
	dir := h.sess.newDir(fname, mode)
	ent, err := h.sess.fs.Create(h.ent, dir)
	if err != nil {
		return noHandle, err
	}
	parents := make([]*FileEnt, len(h.parents)+1)
	copy(parents, h.parents)
	parents[len(h.parents)] = h.ent
	ent.incref() // add ref from this handle:
	return FileHandle{Path: path, ent: ent, sess: h.sess, parents: parents}, nil
}

func (ref *FileEnt) Stat(ctx context.Context) (p9p.Dir, error) {
	return ref.Info, nil
}
func (h FileHandle) Stat(ctx context.Context) (p9p.Dir, error) {
	return h.ent.Stat(ctx)
}

func (ref *FileEnt) WStat(ctx context.Context, dir p9p.Dir) error {
	if dir.Mode != ^uint32(0) {
		ref.Info.Mode = dir.Mode
	}
	if dir.UID != "" {
		ref.Info.UID = dir.UID
	}
	if dir.GID != "" {
		ref.Info.GID = dir.GID
	}
	if dir.Name != "" {
		return ENotImpl
	}
	if dir.Length != ^uint64(0) {
		m := uint64(len(ref.Data))
		if m < dir.Length {
			return p9p.MessageRerror{Ename: "Size larger than file"}
		}
		ref.Data = ref.Data[:dir.Length]
	}
	//if dir.ModTime != time.Time{} || dir.AccessTime != ^uint32(0) {
	//	ref.Info.ModTime = dir.ModTime
	//	ref.Info.AccessTime = dir.AccessTime
	//}

	return nil
}
func (h FileHandle) WStat(ctx context.Context, dir p9p.Dir) error {
	// TODO(frobnitzem): permission check
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
	if offset > n {
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
	ref.Info.Length = uint64(len(ref.Data))
	return int(m), nil
}
func (h FileHandle) Write(ctx context.Context, p []byte,
    offset int64) (n int, err error) {
	return h.ent.Write(ctx, p, offset)
}

func (_ FileHandle) IOUnit() int {
	return 0
}
