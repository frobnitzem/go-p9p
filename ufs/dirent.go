package ufs

import (
	"context"
	"io"
	"os"
	"os/user"
	"path"
	"strconv"
	"syscall"

	p9p "github.com/frobnitzem/go-p9p"
)

func (f *FileRef) IsDir() bool {
	return f.Info.Mode&p9p.DMDIR > 0
}

func (ref *FileRef) Qid() p9p.Qid {
	return ref.Info.Qid
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

func (ref *FileRef) OpenDir(ctx context.Context) (p9p.ReadNext, error) {
	if !ref.IsDir() {
		return nil, p9p.MessageRerror{Ename: "not a directory"}
	}

	files, err := os.ReadDir(ref.fullPath())
	if err != nil {
		return nil, err
	}
	var dirs []p9p.Dir
	for _, info := range files {
		d, err := dirFromEntry(info)
		if err == nil {
			dirs = append(dirs, d)
		}
	}
	return (&dirList{dirs, false}).Next, nil
}

func (ref *FileRef) Clunk(ctx context.Context) error {
	if ref.file != nil {
		return ref.file.Close()
	}
	return nil
}

func (ref *FileRef) Remove(ctx context.Context) error {
	ref.Clunk(ctx)
	if ref.Path == "/" || ref.Path == "\\" {
		return p9p.MessageRerror{Ename: "cannot remove root"}
	}
	return os.Remove(ref.fullPath())
}

func (ref *FileRef) Walk(ctx context.Context, names ...string) ([]p9p.Qid, p9p.Dirent, error) {
	if len(names) == 0 {
		next, err := ref.fs.newRef(ref.Path)
		if err != nil {
			next = nil
		}
		return nil, next, err
	}

	// names is guaranteed to pass p9p.ValidPath
	newpath, err := p9p.WalkName(ref.Path, names...)
	if err != nil {
		return nil, nil, err
	}
	next, err := ref.fs.newRef(newpath)
	// TODO(frobnitzem): return qid-s corresponding to partial walk
	if err != nil {
		return nil, nil, err
	}
	qids := make([]p9p.Qid, len(names))
	qids[len(qids)-1] = next.Qid()
	return qids, next, err
}

func (ref *FileRef) Create(ctx context.Context, name string,
	perm uint32, mode p9p.Flag) (p9p.Dirent, p9p.File, error) {
	// RelName requires name to be in current dir
	newrel, err := p9p.CreateName(ref.Path, name)
	if err != nil {
		return nil, nil, err
	}
	newpath, err := ref.fs.fullPath(newrel)
	if err != nil { // should always succeed
		return nil, nil, err
	}

	var file *os.File
	switch {
	case perm&p9p.DMDIR != 0:
		err = os.Mkdir(newpath, os.FileMode(perm&0777))

	case perm&p9p.DMSYMLINK != 0:
	case perm&p9p.DMNAMEDPIPE != 0:
	case perm&p9p.DMDEVICE != 0:
		err = p9p.MessageRerror{Ename: "not implemented"}

	default:
		file, err = os.OpenFile(newpath, oflags(mode)|os.O_CREATE, os.FileMode(perm&0777))
	}

	if err != nil {
		return nil, nil, err
	}
	ent, err := ref.fs.newRef(newrel)
	if err != nil { // may fail if stat fails.
		if file != nil {
			file.Close()
		}
		return nil, nil, err
	}

	if file != nil {
		ent.file = file
	}

	return ent, ent, nil
}

func (ref *FileRef) Stat(ctx context.Context) (p9p.Dir, error) {
	return ref.Info, nil
}

func (ref *FileRef) WStat(ctx context.Context, dir p9p.Dir) error {
	if dir.Mode != ^uint32(0) {
		err := os.Chmod(ref.fullPath(), os.FileMode(dir.Mode&0777))
		if err != nil {
			return err
		}
	}

	if dir.UID != "" || dir.GID != "" {
		usr, err := user.Lookup(dir.UID)
		if err != nil {
			return err
		}
		uid, err := strconv.Atoi(usr.Uid)
		if err != nil {
			return err
		}
		grp, err := user.LookupGroup(dir.GID)
		if err != nil {
			return err
		}
		gid, err := strconv.Atoi(grp.Gid)
		if err != nil {
			return err
		}
		if err := os.Chown(ref.fullPath(), uid, gid); err != nil {
			return err
		}
	}

	if dir.Name != "" {
		var err error
		var rel string
		if path.IsAbs(dir.Name) {
			rel = path.Clean(rel)
		} else {
			rel = path.Join(path.Dir(ref.Path), dir.Name)
		}
		newpath, err := ref.fs.fullPath(rel)
		if err != nil {
			return err
		}
		if err = syscall.Rename(ref.fullPath(), newpath); err != nil {
			return err
		}
		ref.Path = rel
	}

	if dir.Length != ^uint64(0) {
		if err := os.Truncate(ref.fullPath(), int64(dir.Length)); err != nil {
			return err
		}
	}

	// If either mtime or atime need to be changed, then
	// we must change both.
	//if dir.ModTime != time.Time{} || dir.AccessTime != ^uint32(0) {
	// mt, at := time.Unix(int64(dir.Mtime), 0), time.Unix(int64(dir.Atime), 0)
	// if cmt, cat := (dir.Mtime == ^uint32(0)), (dir.Atime == ^uint32(0)); cmt || cat {
	// 	st, e := os.Stat(fid.path)
	// 	if e != nil {
	// 		req.RespondError(toError(e))
	// 		return
	// 	}
	// 	switch cmt {
	// 	case true:
	// 		mt = st.ModTime()
	// 	default:
	// 		at = atime(st.Sys().(*syscall.Stat_t))
	// 	}
	// }
	// e := os.Chtimes(fid.path, at, mt)
	// if e != nil {
	// 	req.RespondError(toError(e))
	// 	return
	// }
	//}
	return nil
}

func (ref *FileRef) Open(ctx context.Context,
	mode p9p.Flag) (p9p.File, error) {
	file, err := os.OpenFile(ref.fullPath(), oflags(mode), 0)
	if err != nil {
		return nil, err
	}
	ref.file = file
	return ref, err
}

func (ref *FileRef) Read(ctx context.Context, p []byte,
	offset int64) (n int, err error) {
	n, err = ref.file.ReadAt(p, offset)
	if err != nil && err != io.EOF {
		return n, err
	}
	return n, nil
}

func (ref *FileRef) Write(ctx context.Context, p []byte,
	offset int64) (n int, err error) {
	return ref.file.WriteAt(p, offset)
}

func (ref *FileRef) IOUnit() int {
	return 0
}
