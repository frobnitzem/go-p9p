package ufs

import (
	"os"
	"os/user"
    "context"
	"io"
	"io/ioutil"
	"path/filepath"
	"syscall"
	"strconv"

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

func (ref *FileRef) Entries(ctx context.Context) ([]p9p.Dir, error) {
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
    return dirs, nil
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

func (ref *FileRef) Clone(ctx context.Context) (p9p.Dirent, error) {
	return ref.fs.newRef(ref.Path)
}

func (ref *FileRef) Walk(ctx context.Context, name string) (p9p.Dirent, error) {
    // name is guaranteed not to contain '/'
    // Properly handles .. since Path starts with '/'
	newpath := relName(ref.Path, name)
	return ref.fs.newRef(newpath)
}

func (ref *FileRef) Create(ctx context.Context, name string,
                           perm uint32, mode p9p.Flag) (p9p.Dirent, error) {
    var err error
    newrel := relName(ref.Path, name)
	newpath := filepath.Join(ref.fs.Base, newrel)

    var file *os.File
	switch {
	case perm&p9p.DMDIR != 0:
		err = os.Mkdir(newpath, os.FileMode(perm&0777))

	case perm&p9p.DMSYMLINK != 0:
	case perm&p9p.DMNAMEDPIPE != 0:
	case perm&p9p.DMDEVICE != 0:
		err = p9p.MessageRerror{Ename: "not implemented"}

	default:
        // TODO(frobnitzem): Stash open file somewhere, since
        // we know create will be immediately followed by open,
        // but only for files.
        file, err = os.OpenFile(newpath, oflags(mode)|os.O_CREATE, os.FileMode(perm&0777))
        file.Close()
	}

	if err != nil {
		return nil, err
	}

    ent, err := ref.fs.newRef(newrel)
    if err != nil && file != nil {
        file.Close()
    }
    return ent, err
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
        rel := relName(filepath.Dir(ref.Path), dir.Name)

		newpath := filepath.Join(ref.fs.Base, rel)
		if err := syscall.Rename(ref.fullPath(), newpath); err != nil {
			return nil
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
