package p9p

import (
	"context"
	"sync"
)

// TODO(frobnitzem): Make these methods return errors
// listed in errors.go where possible.

// FileSystem implementions gather more data than the required
// minimal information to send messages.  The following expanded
// interfaces are generated and used internally.
// They contain useful information that can be looked up
// from the server, but are only guaranteed to be active while
// an active call is running on the server.
//
// Internal representation of a Dirent
// File is nil unless Open has been called.
//
// Path is an internal path.  Internal paths have the invariants:
//
//   - Path always begins with "/".
//   - Path does not contain any "\\" characters.
//   - Path never contains "." or ".." or "" (empty) elements.
//
// The lock is used during all operations on the SFid
// -- especially during creation and Opening
// to protect cases where an inconsistent state of the
// SFid may be seen.
//
// getRef acquires the lock before returning it, to
// ensure that the receiver sees such updates.
type SFid struct {
	sync.Mutex
	Ent  Dirent // non-nil if unlocked (unless Ent deleted before being defined)
	File File   // non-nil if Open-ed
	// if file was returned by Auth, must also implement AuthFile
	path string // Unix-convention, "clean" absolute path within the FileSys
	// This is an *internal path*.
	// at the time the Ent was created by a Walk/Create.
	// If modified by the server, changes will only
	// apply to future Walk/Create-s from this Ent.
	Mode Flag // Defined if Open-ed.
}

type session struct {
	fs   FileSys
	refs sync.Map // type [Fid](*SFid)
}

// TODO(frobnitzem): validate required server returns to ensure non-nil.
func EnsureNonNil(x interface{}, err error) error {
	if err != nil {
		return err
	}
	if x == nil {
		return MessageRerror{Ename: "invalid result"}
	}
	return nil
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
		fs: fs,
	}
}

func (sess *session) Stop(err error) error {
	ctx := CancelledCtxt{}
	sess.refs.Range(func(fid, ref1 interface{}) bool {
		ref, ok := ref1.(*SFid)
		if ok && ref.Ent != nil { // close and clunk
			delRefAction(ctx, ref, false)
		}
		return true
	})
	return err
}

// Acquires a lock on the SFid just after successful lookup.
// A successful return means the caller is responsible
// for calling `ref.Unlock()`.
func (sess *session) getRef(fid Fid) (*SFid, error) {
	if fid == NOFID {
		return nil, ErrUnknownfid
	}

	ref1, found := sess.refs.Load(fid)
	if !found {
		return nil, ErrUnknownfid
	}
	ref, _ := ref1.(*SFid)

	ref.Lock()
	// Guard against deletion just after our lookup.
	if ref.Ent == nil {
		ref.Unlock()
		return nil, ErrUnknownfid
	}

	return ref, nil
}

// Sets ref.Ent (could also informs the ent of ref.)
func (ref *SFid) link(ent Dirent) {
	ref.Ent = ent
}

// Add a new reference to the refs table.
// -- called by auth, attach and walk
//
// Note - Each SFid corresponds to exactly one Fid.
// The fid can be opened exactly once.  We assume
// that the client is tracking number of processes referencing
// each Fid, so we don't need to handle ref-counting in the server.
//
// If no error is returned, an SFid (created in the locked state)
// is returned, still in the locked state.
// The caller is responsible for unlocking it.
//
// The SFid starts with with Ent == nil.
//
// getRef checks for this nil-Ent, and returns ErrUnknownfid.
// This covers the case where a fid was put on hold, but
// creation of an Ent wasn't successful, so the fid
// was removed from the map.
func (sess *session) newRef(fid Fid) (ref *SFid, err error) {
	if fid == NOFID {
		return nil, ErrUnknownfid
	}

	ref = &SFid{}
	ref.Lock()
	_, found := sess.refs.LoadOrStore(fid, ref)
	if found {
		//ref.Unlock() not needed
		return nil, ErrDupfid
	}

	return ref, nil
}

// Delete reference from the refs table.
// If remove is true, calls Dirent.Remove.
// Otherwise, calls Dirent.Clunk.
// TODO(frobnitzem): determine whether aFid can be clunked.
// This code won't see it, so will return an error here in this case.
func (sess *session) delRef(ctx context.Context, fid Fid,
	remove bool) error {

	ref1, found := sess.refs.LoadAndDelete(fid)
	if !found {
		return ErrUnknownfid
	}
	ref, _ := ref1.(*SFid)

	ref.Lock()
	defer ref.Unlock()
	if ref.Ent == nil {
		return nil
	}

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

func delRefAction(ctx context.Context, ref *SFid, remove bool) error {
	var err error
	var err2 error
	if remove {
		err2 = ref.Ent.Remove(ctx)
	} else {
		err2 = ref.Ent.Clunk(ctx)
	}
	ref.Ent = nil
	return combine_errors(err, err2)
}

// Creates a new auth fid.
// These aref-s are locked until the afile is established.
func (sess *session) Auth(ctx context.Context, afid Fid,
	uname, aname string) (Qid, error) {

	// FIXME: need a mutex to generate unique paths here...
	aq := Qid{Type: QTAUTH, Path: 0}

	if afid == NOFID { // not in spec, but treat as a no-op
		return aq, nil
	}
	if !sess.fs.RequireAuth(ctx) {
		return aq, MessageRerror{Ename: "no auth"}
	}

	aref, err := sess.newRef(afid)
	if err != nil {
		return aq, err
	}
	defer aref.Unlock()

	afile, err := sess.fs.Auth(ctx, uname, aname)
	if err != nil { // need to re-acquire session lock to delete
		sess.refs.Delete(afid)
		return aq, err
	}
	//file, _ := afile.(File)
	aref.File = afile

	return aq, err
}

func (sess *session) Attach(ctx context.Context, fid, afid Fid,
	uname, aname string) (Qid, error) {
	// Not in the spec, but if we're to create files...
	//if uname == "" {
	//	return Qid{}, MessageRerror{Ename: "no user"}
	//}

	var af AuthFile

	if afid != NOFID {
		var aref *SFid
		var ok bool
		var err error

		aref, err = sess.getRef(afid)
		if err != nil || aref.File == nil {
			return Qid{}, ErrUnknownfid
		}
		defer aref.Unlock()

		af, ok = aref.File.(AuthFile)
		if !ok {
			return Qid{}, ErrUnknownfid
		}

		if !af.Success() {
			return Qid{}, ErrPerm
		}
	}

	ref, err := sess.newRef(fid)
	if err != nil {
		return Qid{}, err
	}
	defer ref.Unlock()

	ent, err := sess.fs.Attach(ctx, uname, aname, af)
	if err != nil {
		sess.refs.Delete(fid)
		return Qid{}, err
	}
	ref.link(ent)

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

	var newref *SFid

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
			sess.refs.Delete(newfid)
			newref.Unlock()
		}
	}()

	if newfid != fid {
		newref, err = sess.newRef(newfid)
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
		_, ent, err = ref.Ent.Walk(ctx)
		err = EnsureNonNil(ent, err)
		if err != nil {
			return nil, err
		}
	} else {
		var err error

		if !IsDir(ref.Ent) {
			err = MessageRerror{Ename: "not a directory"}
			return nil, err
		}
		qids, ent, err = ref.Ent.Walk(ctx, names...)
		err = EnsureNonNil(ent, err)
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
		ref.Ent.Clunk(ctx) // TODO(frobnitzem): note - ignoring error here
	} else {
		// We have increased the size of sess.refs by 1.
		// both ref and newref are locked
		ref.Unlock()
		ref = newref
		newref = nil
		// cleanup will unlock ref
	}
	ref.link(ent)
	return qids, nil
}

func (sess *session) Read(ctx context.Context, fid Fid, p []byte, offset int64) (n int, err error) {
	ref, err := sess.getRef(fid)
	if err != nil {
		return 0, err
	}
	defer ref.Unlock()
	if ref.File == nil {
		return 0, MessageRerror{Ename: "no file open"} //ErrClosed
	}
	if (ref.Mode & OEXEC) == OWRITE {
		return 0, ErrNoread
	}

	return ref.File.Read(ctx, p, offset)
}

func (sess *session) Write(ctx context.Context, fid Fid, p []byte,
	offset int64) (n int, err error) {
	ref, err := sess.getRef(fid)
	if err != nil {
		return 0, err
	}
	defer ref.Unlock()
	if ref.File == nil {
		return 0, MessageRerror{Ename: "no file open"} //ErrClosed
	}
	if (ref.Mode&OEXEC) != OWRITE && (ref.Mode&OEXEC) != ORDWR {
		return 0, ErrNowrite
	}

	return ref.File.Write(ctx, p, offset)
}

func (sess *session) Open(ctx context.Context, fid Fid,
	mode Flag) (Qid, uint32, error) {
	ref, err := sess.getRef(fid)
	if err != nil {
		return Qid{}, 0, err
	}
	defer ref.Unlock()

	err = openLocked(ctx, ref, mode)
	if err != nil {
		return Qid{}, 0, err
	}

	return ref.Ent.Qid(), uint32(ref.File.IOUnit()), nil
}

func (_ *Readdir) IOUnit() int {
	return 0
}

// Sets ref.File if successful.
// Note: This does not check file permissions
//
//	before opening!  It is up to the caller.
//
// Should be called with ref-lock held.
// Does not release the lock.
func openLocked(ctx context.Context, ref *SFid, mode Flag) error {

	if ref.File != nil {
		return MessageRerror{Ename: "already open"}
	}

	var file File
	var err error

	if IsDir(ref.Ent) {
		dirs, err := ref.Ent.OpenDir(ctx)
		err = EnsureNonNil(dirs, err)
		if err != nil {
			return err
		}
		file = NewReaddir(NewCodec(), dirs)
	} else {
		file, err = ref.Ent.Open(ctx, mode)
		err = EnsureNonNil(file, err)
		if err != nil {
			return err
		}
	}

	ref.File = file
	ref.Mode = mode
	return nil
}

func (sess *session) Create(ctx context.Context, parent Fid, name string,
	perm uint32, mode Flag) (Qid, uint32, error) {
	var err error
	var ref *SFid
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

	if !IsDir(ref.Ent) {
		return fail("create in non-directory")
	}

	ent, file, err := ref.Ent.Create(ctx, name, perm, mode)
	err = EnsureNonNil(ent, err)
	err = EnsureNonNil(file, err)
	if err != nil {
		return fail(err.Error())
	}
	if IsDir(ent) { // Do our own thing for directories.
		next := SFid{Ent: ent}
		err = openLocked(ctx, &next, mode)
		if err != nil {
			ent.Clunk(ctx) // Note: ignoring possible multiple errors
			return fail(err.Error())
		}
		file = next.File
	}

	// Success. Clean-up ref and replace with ent.
	ref.Ent.Clunk(ctx)
	ref.File = nil
	ref.Mode = 0
	ref.link(ent)
	ref.File = file
	ref.Mode = mode

	return ref.Ent.Qid(), uint32(file.IOUnit()), nil
}

func (sess *session) Stat(ctx context.Context, fid Fid) (Dir, error) {
	ref, err := sess.getRef(fid)
	if err != nil {
		return Dir{}, err
	}
	defer ref.Unlock()

	return ref.Ent.Stat(ctx)
}

func (sess *session) WStat(ctx context.Context, fid Fid, dir Dir) error {
	ref, err := sess.getRef(fid)
	if err != nil {
		return err
	}
	defer ref.Unlock()

	return ref.Ent.WStat(ctx, dir)
}

func (sess *session) Version() (msize int, version string) {
	return DefaultMSize, DefaultVersion
}
