package ufs

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"os/user"
	"path"
	"strconv"
	"syscall"

	p9p "github.com/frobnitzem/go-p9p"
)

type fWrap struct {
	File *os.File
}

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

	files, err := ioutil.ReadDir(ref.fullPath())
	if err != nil {
		return nil, err
	}
	var dirs []p9p.Dir
	for _, info := range files {
		dirs = append(dirs, dirFromInfo(info))
	}
	return (&dirList{dirs, false}).Next, nil
}

func (ref *FileRef) Clunk(ctx context.Context) error {
	return nil
}

func (ref *FileRef) Remove(ctx context.Context) error {
	if ref.Path == "/" || ref.Path == "\\" {
		return p9p.MessageRerror{Ename: "cannot remove root"}
	}
	return os.Remove(ref.fullPath())
}

func (ref *FileRef) Walk(ctx context.Context, names ...string) ([]p9p.Qid, p9p.Dirent, error) {
	if len(names) == 0 {
		next, err := ref.fs.newRef(ref.Path)
		if err != nil {
			return nil, nil, err
		}
		return nil, next, err
	}

	// names is guaranteed to pass p9p.ValidPath
	newpath, err := relName(ref.Path, names...)
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
	// relName requires name to be in current dir
	newrel, err := relName(ref.Path, name)
	if err != nil {
		return nil, nil, err
	}
	newpath, err := ref.fs.fullPath(newrel)
	if err != nil {
		return nil, nil, err
	}

	var f *os.File
	switch {
	case perm&p9p.DMDIR != 0:
		err = os.Mkdir(newpath, os.FileMode(perm&0777))

	case perm&p9p.DMSYMLINK != 0:
	case perm&p9p.DMNAMEDPIPE != 0:
	case perm&p9p.DMDEVICE != 0:
		err = p9p.MessageRerror{Ename: "not implemented"}

	default:
		f, err = os.OpenFile(newpath, oflags(mode)|os.O_CREATE, os.FileMode(perm&0777))
	}

	if err != nil {
		return nil, nil, err
	}
	var file p9p.File
	if f != nil {
		file = &fWrap{File: f}
	}
	ent, _ := ref.fs.newRef(newrel)

	return ent, file, nil
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
		if !path.IsAbs(dir.Name) {
			rel = path.Join(path.Dir(ref.Path), dir.Name)
		}
		rel = path.Clean(rel)
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
	f, err := os.OpenFile(ref.fullPath(), oflags(mode), 0)
	if err != nil {
		return nil, err
	}

	return &fWrap{File: f}, err
}

func (file *fWrap) Read(ctx context.Context, p []byte,
	offset int64) (n int, err error) {
	n, err = file.File.ReadAt(p, offset)
	if err != nil && err != io.EOF {
		return n, err
	}
	return n, nil
}

func (file *fWrap) Write(ctx context.Context, p []byte,
	offset int64) (n int, err error) {
	return file.File.WriteAt(p, offset)
}

func (file *fWrap) Close(ctx context.Context) error {
	file.File.Close()
	return nil
}

func (_ *fWrap) IOUnit() int {
	return 0
}
