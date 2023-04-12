package p9p

import (
	"context"
	"sync"
)

// TODO(frobnitzem): Make these methods return errors
// listed in errors.go where possible.

// Internal representation of a Dirent
// File is nil unless Open has been called.
//
// The lock is used during Dirent or File creation
// to protect cases where an inconsistent state of the
// dirEnt may be seen.
//
// getRef acquires the lock before returning it, to
// ensure that the receiver sees such updates.
//
// By creating a struct here, we can prevent holding the
// lock on the session.
type dirEnt struct {
	sync.Mutex
	ent  Dirent // non-nil if unlocked
	file File   // non-nil if Open-ed
}

type authEnt struct {
	sync.Mutex
	afile AuthFile
	uname string
	aname string
}

type session struct {
	sync.Mutex
	fs    FileSys
	auths map[Fid]*authEnt
	refs  map[Fid]*dirEnt
}

// TODO(frobnitzem): Implement a client that uses this API to call
// the server functions directly.

// Create a session object able to respond to 9P calls.
// Fid-s are managed at this level, so the FileSys only
// works with Dirent-s.  All operations on a
// Fid are transactional, since a lock is held while server
// code is doing something to the corresponding Dirent.
//
//	For example, p9p/ufs translates all fid-s using sess.getRef(fid)
//	and https://9fans.github.io/usr/local/plan9/src/cmd/ramfs.c
//	uses user-defined structs for Fid-s.
func SFileSys(fs FileSys) Session {
	return &session{
		fs:    fs,
		auths: make(map[Fid]*authEnt),
		refs:  make(map[Fid]*dirEnt),
	}
}

func (sess *session) Stop(err error) error {
	ctx := CancelledCtxt{}
	for _, ref := range sess.refs {
		// close and clunk
		delRefAction(ctx, ref, false)
	}
	return err
}

// Acquires a lock on the dirEnt just after successful lookup.
// A successful return means the caller is responsible
// for calling `ref.Unlock()`.
func (sess *session) getRef(fid Fid) (*dirEnt, error) {
	if fid == NOFID {
		return nil, ErrUnknownfid
	}

	sess.Lock()

	ref, found := sess.refs[fid]
	if !found {
		sess.Unlock()
		return nil, ErrUnknownfid
	}
	sess.Unlock()

	ref.Lock()
	// Guard against deletion just after our lookup.
	if ref.ent == nil {
		ref.Unlock()
		return nil, ErrUnknownfid
	}

	return ref, nil
}

// Called with the session locked.
// Ensure that this reference is valid to be used
// as a new reference.
func (sess *session) checkRefLocked(fid Fid) error {
	if fid == NOFID {
		return ErrUnknownfid
	}

	_, found := sess.refs[fid]
	if found {
		return ErrDupfid
	}

	return nil
}

// Add a new reference to the refs table.
// -- called by attach and walk
//
// Note - Each dirEnt corresponds to exactly one Fid.
// The fid can be opened exactly once.  We assume
// that the client is tracking number of processes referencing
// each Fid, so we don't need to handle ref-counting in the server.
func (sess *session) newRef(fid Fid, d Dirent) error {
	sess.Lock()
	defer sess.Unlock()

	if err := sess.checkRefLocked(fid); err != nil {
		return err
	}

	sess.refs[fid] = &dirEnt{ent: d}
	return nil
}

// Create a new, empty, reference set to locked on creation.
// All (unlockd) dirEnts must have non-nil ref.ent values.
// It is the caller's responsibility to fill that in, and then
// unlock it.
func (sess *session) holdRef(fid Fid) (*dirEnt, error) {
	sess.Lock()
	defer sess.Unlock()

	if err := sess.checkRefLocked(fid); err != nil {
		return nil, err
	}

	ref := &dirEnt{}
	ref.Lock()
	sess.refs[fid] = ref
	return ref, nil
}

// Delete reference from the refs table.
// If remove is true, calls Dirent.Remove.
// Otherwise, calls Dirent.Clunk.
// TODO(frobnitzem): determine whether aFid can be clunked.
// This code won't see it, so will return an error here in this case.
func (sess *session) delRef(ctx context.Context, fid Fid,
	remove bool) error {
	sess.Lock()

	ref, found := sess.refs[fid]
	if !found {
		sess.Unlock()
		return ErrUnknownfid
	}
	sess.Unlock()

	ref.Lock()  // WARNING: We must ensure that no code acquires these
	sess.Lock() //         in reverse order.
	delete(sess.refs, fid)
	sess.Unlock()
	defer ref.Unlock()

	return delRefAction(ctx, ref, remove)
}

func combine_errors(err, err2 error) error {
	if err == nil {
		return err2
	}
	if err2 == nil {
		return err
	}
	return MessageRerror{Ename: err.Error() + "/" + err2.Error()}
}

func delRefAction(ctx context.Context, ref *dirEnt, remove bool) error {
	var err error
	var err2 error
	if remove {
		err2 = ref.ent.Remove(ctx)
	} else {
		err2 = ref.ent.Clunk(ctx)
	}
	ref.ent = nil
	return combine_errors(err, err2)
}

// Creates a new auth fid.
// These aref-s are locked until the afile is established.
func (sess *session) Auth(ctx context.Context, afid Fid,
	uname, aname string) (Qid, error) {
	// Can't close/clunk an afid, so the path will be unique.
	aq := Qid{Type: QTAUTH, Path: uint64(afid)}

	if afid == NOFID { // not in spec, but treat as a no-op
		return aq, nil
	}
	if !sess.fs.RequireAuth(ctx) {
		return aq, MessageRerror{Ename: "no auth"}
	}

	sess.Lock()
	_, found := sess.auths[afid]
	if found {
		sess.Unlock()
		return aq, ErrDupfid
	}
	aref := &authEnt{uname: uname, aname: aname}

	aref.Lock()
	sess.auths[afid] = aref
	sess.Unlock()

	var err error
	aref.afile, err = sess.fs.Auth(ctx, uname, aname)
	if err != nil { // need to re-acquire session lock to delete
		aref.afile = nil
	}
	aref.Unlock()

	if err != nil {
		sess.Lock()
		aref.Lock()
		delete(sess.auths, afid)
		aref.Unlock()
		sess.Unlock()
	}

	return aq, err
}

func (sess *session) Attach(ctx context.Context, fid, afid Fid,
	uname, aname string) (Qid, error) {
	// Not in the spec, but if we're to create files...
	//if uname == "" {
	//	return Qid{}, MessageRerror{Ename: "no user"}
	//}

	var aref *authEnt
	var af AuthFile

	if afid != NOFID {
		sess.Lock()
		var found bool
		aref, found = sess.auths[afid]
		sess.Unlock()
		if !found {
			return Qid{}, ErrUnknownfid
		}

		aref.Lock() // acquiring in non-nil state guarantees afile is present
		if aref.afile == nil {
			aref.Unlock()
			return Qid{}, ErrUnknownfid
		}
		ok := aref.afile.Success()
		af = aref.afile
		aref.Unlock()

		if !ok {
			return Qid{}, MessageRerror{Ename: "unauthorized"}
		}
	}

	ref, err := sess.holdRef(fid)
	if err != nil {
		return Qid{}, err
	}

	ent, err := sess.fs.Attach(ctx, uname, aname, af)
	if err != nil {
		sess.Lock()
		delete(sess.refs, fid)
		sess.Unlock()
		return Qid{}, err
	}
	ref.ent = ent
	ref.Unlock()

	return ent.Qid(), nil
}

func (sess *session) Clunk(ctx context.Context, fid Fid) error {
	return sess.delRef(ctx, fid, false)
}

func (sess *session) Remove(ctx context.Context, fid Fid) error {
	return sess.delRef(ctx, fid, true)
}

func (sess *session) Walk(ctx context.Context, fid Fid, newfid Fid,
	names ...string) ([]Qid, error) {

	bsp := ValidPath(names)
	if bsp < 0 { // check that path is normalized
		return nil, MessageRerror{Ename: "Non-normalized path"}
	}
	var qids []Qid
	var ent Dirent // the newly discovered ent

	var newref *dirEnt

	ref, err := sess.getRef(fid)
	if err != nil {
		return nil, err
	}

	// lookup ref. from inside the clean-up function
	defer func() {
		ref.Unlock()
		// newref must be nil by the time
		// the function returns (or else it's deleted)
		// (we'll swap it with ref if/when needed)
		if newref != nil {
			sess.Lock()
			delete(sess.refs, newfid)
			sess.Unlock()
		}
	}()

	if newfid != fid {
		newref, err = sess.holdRef(newfid)
		if err != nil {
			return nil, err
		}
	}

	// Both paths below must define ent and qids (or else return nil,err)
	if len(names) == 0 { // Clone
		if newfid == fid { // a no-op
			return nil, nil
		}
		var err error
		_, ent, err = ref.ent.Walk(ctx)
		if err != nil {
			return nil, err
		}
	} else {
		var err error

		if !IsDir(ref.ent) {
			err = MessageRerror{Ename: "not a directory"}
			return nil, err
		}
		qids, ent, err = ref.ent.Walk(ctx, names...)
		if err != nil {
			return nil, err
		}
		// An error walking the first path element gets propagated.
		if len(qids) == 0 {
			return nil, err
		}
	}
	// "Only if it is equal, however, will newfid be affected"
	if len(qids) != len(names) {
		return qids, nil
	}

	if newfid == fid {
		// Re-use fid for result of walk.
		// Note: It is still locked.
		ref.ent.Clunk(ctx) // TODO(frobnitzem): note - ignoring error here
	} else {
		// We have increased the size of sess.refs by 1.
		// both ref and newref are locked
		ref.Unlock()
		ref = newref
		newref = nil
		// cleanup will unlock ref
	}
	ref.ent = ent
	return qids, nil
}

func (sess *session) Read(ctx context.Context, fid Fid, p []byte, offset int64) (n int, err error) {
	ref, err := sess.getRef(fid)
	if err != nil {
		return 0, err
	}
	defer ref.Unlock()
	if ref.file == nil {
		return 0, MessageRerror{Ename: "no file open"} //ErrClosed
	}

	return ref.file.Read(ctx, p, offset)
}

func (sess *session) Write(ctx context.Context, fid Fid, p []byte,
	offset int64) (n int, err error) {
	ref, err := sess.getRef(fid)
	if err != nil {
		return 0, err
	}
	defer ref.Unlock()
	if ref.file == nil {
		return 0, MessageRerror{Ename: "no file open"} //ErrClosed
	}

	return ref.file.Write(ctx, p, offset)
}

func (sess *session) Open(ctx context.Context, fid Fid,
	mode Flag) (Qid, uint32, error) {
	ref, err := sess.getRef(fid)
	if err != nil {
		return Qid{}, 0, err
	}
	defer ref.Unlock()

	// TODO(frobnitzem): check open permissions here,
	// before calling openLocked.
	err = openLocked(ctx, ref, mode)
	if err != nil {
		return Qid{}, 0, err
	}

	return ref.ent.Qid(), uint32(ref.file.IOUnit()), nil
}

func (_ *Readdir) IOUnit() int {
	return 0
}

// Sets ref.file if successful.
// Note: This does not check file permissions
//
//	before opening!  It is up to the caller.
//
// Should be called with ref-lock held.
// Does not release the lock.
func openLocked(ctx context.Context, ref *dirEnt,
	mode Flag) error {

	if ref.file != nil {
		return MessageRerror{Ename: "already open"}
	}

	var file File
	var err error

	if IsDir(ref.ent) {
		dirs, err := ref.ent.OpenDir(ctx)
		if err != nil {
			return err
		}
		file = NewReaddir(NewCodec(), dirs)
	} else {
		file, err = ref.ent.Open(ctx, mode)
		if err != nil {
			return err
		}
	}

	ref.file = file
	return nil
}

func (sess *session) Create(ctx context.Context, parent Fid, name string,
	perm uint32, mode Flag) (Qid, uint32, error) {
	var err error
	var ref *dirEnt
	var file File

	fail := func(name string) (Qid, uint32, error) {
		return Qid{}, 0, MessageRerror{Ename: name}
	}

	if name == "." || name == ".." {
		return fail("illegal filename")
	}

	ref, err = sess.getRef(parent)
	if err != nil {
		return fail(err.Error())
	}
	defer ref.Unlock()

	if !IsDir(ref.ent) {
		return fail("create in non-directory")
	}

	ent, file, err := ref.ent.Create(ctx, name, perm, mode)
	if err != nil {
		return fail(err.Error())
	}
	if IsDir(ent) { // Do our own thing for directories.
		next := dirEnt{ent: ent}
		err = openLocked(ctx, &next, mode)
		if err != nil {
			ent.Clunk(ctx) // Note: ignoring possible multiple errors
			return fail(err.Error())
		}
		file = next.file
	}

	// Success. Clean-up ref and replace with ent.
	ref.ent.Clunk(ctx)
	ref.ent = ent
	ref.file = file

	return ref.ent.Qid(), uint32(file.IOUnit()), nil
}

func (sess *session) Stat(ctx context.Context, fid Fid) (Dir, error) {
	ref, err := sess.getRef(fid)
	if err != nil {
		return Dir{}, err
	}
	defer ref.Unlock()

	return ref.ent.Stat(ctx)
}

func (sess *session) WStat(ctx context.Context, fid Fid, dir Dir) error {
	ref, err := sess.getRef(fid)
	if err != nil {
		return err
	}
	defer ref.Unlock()

	return ref.ent.WStat(ctx, dir)
}

func (sess *session) Version() (msize int, version string) {
	return DefaultMSize, DefaultVersion
}
