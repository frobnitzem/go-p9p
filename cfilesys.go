package p9p

import (
	"bytes"
	"context"
	"io"
	"strings"
)

// State of a filesystem as seen from the client side.
type fsState struct {
	session Session
	//fids	map[*cEnt]Fid
	nextfid Fid  // newFid increments, then returns
	root    cEnt // what holds the rootfid
}

func CFileSys(session Session) FileSys {
	return &fsState{session: session, nextfid: Fid(0)}
}

// AuthFile interface
type aFile struct {
	session Session
	afid    Fid
}

var noAuth aFile = aFile{session: nil, afid: NOFID}

// Cannot be programmatically determined from the client side.
func (af aFile) Success() bool {
	return false
}
func (af aFile) Close(ctx context.Context) error {
	return nil
}
func (af aFile) Read(ctx context.Context, p []byte, offset int64) (int, error) {
	return af.session.Read(ctx, af.afid, p, offset)
}
func (af aFile) Write(ctx context.Context, p []byte, offset int64) (int, error) {
	return af.session.Write(ctx, af.afid, p, offset)
}
func (af aFile) IOUnit() int {
	msize, _ := af.session.Version()
	return msize - 11
}

// Cannot be programmatically determined from the client side.
func (fs *fsState) RequireAuth(ctx context.Context) bool {
	return false
	// Attempt a NOFID auth?
	//_, err := fs.session.Auth(ctx, NOFID, "anonymous", "/")
	//return err != nil
}
func (fs *fsState) Auth(ctx context.Context, uname, aname string,
) (AuthFile, error) {
	aFid := fs.newFid()
	_, err := fs.session.Auth(ctx, aFid, uname, aname)
	if err != nil {
		return noAuth, err
	}
	return aFile{session: fs.session, afid: aFid}, nil
}

// Initializes a session by sending an Attach,
// and storing all the relevant session data.
// Does no cleanup (assumes no old state).
func (fs *fsState) Attach(ctx context.Context, uname, aname string,
	af AuthFile) (Dirent, error) {
	rootFid := fs.newFid()

	var aFid Fid
	if af == nil {
		aFid = NOFID
	} else {
		af1, ok := af.(aFile)
		if !ok {
			return noEnt, ErrUnknownfid
		}
		aFid = af1.afid
	}

	qid, err := fs.session.Attach(ctx, rootFid, aFid, uname, aname)
	if err != nil {
		return noEnt, err
	}

	//fs.fids = make(map[*cEnt]Fid)
	return cEnt{
		path: make([]string, 0),
		fid:  rootFid,
		qid:  qid,
		fs:   fs,
	}, nil
}

func (fs *fsState) newFid() Fid {
	fs.nextfid++
	return fs.nextfid
}

type Warning struct {
	s string
}

func (w Warning) Error() string {
	return w.s
}

type cEnt struct {
	path []string // absolute path
	fid  Fid
	qid  Qid // FIXME(frobnitzem): stash qids here
	fs   *fsState
}

var noEnt cEnt = cEnt{nil, NOFID, Qid{}, nil}

type fileRef struct {
	cEnt
	iounit int
}

var noFile fileRef = fileRef{noEnt, 0}

// New entry has no path set yet!
func (fs *fsState) newEnt() cEnt {
	return cEnt{
		fid: fs.newFid(),
		fs:  fs,
	}
}

func (_ cEnt) SetInfo(_ *SFid) {}

// Note: This always returns returns a file with a nonzero IOUnit.
func (ent cEnt) Open(ctx context.Context, mode Flag) (File, error) {
	_, iounit, err := ent.fs.session.Open(ctx, ent.fid, mode)
	iou := int(iounit)
	if iounit < 1 {
		msize, _ := ent.fs.session.Version()
		// size of message max minus fcall io header (Rread)
		iou = msize - 11
	}
	return fileRef{ent, iou}, err
}

func (f fileRef) Read(ctx context.Context, p []byte, offset int64) (int, error) {
	return f.fs.session.Read(ctx, f.fid, p, offset)
}
func (f fileRef) Write(ctx context.Context, p []byte, offset int64) (int, error) {
	return f.fs.session.Write(ctx, f.fid, p, offset)
}
func (f fileRef) IOUnit() int {
	return f.iounit
}
func (f fileRef) Close(ctx context.Context) error {
	return nil
}

type openDir struct {
	fileRef
	done  bool
	nread int64
	buf   []byte
}

// Note: This always returns returns a file with a nonzero IOUnit,
// (because cEnt.Open does)
func (ent cEnt) OpenDir(ctx context.Context) (ReadNext, error) {
	file, err := ent.Open(ctx, OREAD)
	if err != nil {
		return nil, err
	}
	ref, ok := file.(fileRef)
	if !ok {
		return nil, MessageRerror{"Invalid return value from Open"}
	}
	dir := openDir{
		ref,
		false,
		0,
		make([]byte, file.IOUnit()),
	}
	return dir.Next, nil
}

func (dir *openDir) Next(ctx context.Context) ([]Dir, error) {
	if dir.done {
		return nil, nil
	}
	var n int
	var err error

	n, err = dir.Read(ctx, dir.buf, dir.nread)
	if err != nil {
		if err == io.EOF {
			dir.done = true
			return nil, nil
		}
		return nil, err
	}
	dir.nread += int64(n)

	rd := bytes.NewReader(dir.buf[:n])
	codec := NewCodec() // TODO(stevvooe): Need way to resolve codec based on session.
	ret := make([]Dir, 0, 10)
	for {
		var d Dir
		if err = DecodeDir(codec, rd, &d); err != nil {
			if err == io.EOF {
				err = nil
			}
			break
		}
		ret = append(ret, d)
	}
	if len(ret) == 0 {
		dir.done = true
	}
	return ret, err
}

func (ent cEnt) Qid() Qid {
	return ent.qid
}

func (ent cEnt) Create(ctx context.Context, name string,
	perm uint32, mode Flag) (Dirent, File, error) {
	if len(name) == 0 || name == "." || name == ".." || strings.Contains(name, "/\\") {
		return noEnt, noFile, MessageRerror{"Invalid filename"}
	}
	if !IsDir(ent) {
		return noEnt, noFile, ErrCreatenondir
	}
	qid, iounit, err := ent.fs.session.Create(ctx, ent.fid, name, perm, mode)
	if err != nil {
		return noEnt, noFile, err
	}
	ent.path = append(ent.path, name)
	ent.qid = qid

	// TODO(frobnitzem): this appears twice, make a fileEnt function.
	iou := int(iounit)
	if iounit < 1 {
		msize, _ := ent.fs.session.Version()
		// size of message max minus fcall io header (Rread)
		iou = msize - 11
	}
	return ent, fileRef{ent, iou}, err
}
func (ent cEnt) Stat(ctx context.Context) (Dir, error) {
	return ent.fs.session.Stat(ctx, ent.fid)
}
func (ent cEnt) WStat(ctx context.Context, stat Dir) error {
	return ent.fs.session.WStat(ctx, ent.fid, stat)
}
func (ent cEnt) Clunk(ctx context.Context) error {
	return ent.fs.session.Clunk(ctx, ent.fid)
}
func (ent cEnt) Remove(ctx context.Context) error {
	return ent.fs.session.Remove(ctx, ent.fid)
}
func (ent cEnt) Walk(ctx context.Context,
	names ...string) ([]Qid, Dirent, error) {
	steps, bsp := NormalizePath(names)
	if bsp < 0 || bsp > len(ent.path) {
		return nil, ent, MessageRerror{"invalid path: " + strings.Join(names, "/")}
	}

	next := ent.fs.newEnt()
	qids, err := ent.fs.session.Walk(ctx, ent.fid, next.fid, steps...)
	if err != nil {
		return nil, noEnt, err
	}
	if len(qids) != len(names) { // incomplete = failure to get new ent
		return qids, noEnt, Warning{"Incomplete walk result"}
	}
	// drop part of ent.path
	steps = steps[:len(qids)]
	remain := len(ent.path) - bsp
	next.path = append(ent.path[:remain], steps[bsp:]...)
	if len(qids) > 0 {
		next.qid = qids[len(qids)-1]
	} else {
		next.qid = ent.qid
	}

	return qids, next, nil
}
