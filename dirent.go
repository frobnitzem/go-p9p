package p9p

import (
	"context"
	"sync"
)

// Simplified interface to a file that has been Open-ed.
type File interface {
	Read (ctx context.Context, p []byte, offset int64) (int, error)
	Write(ctx context.Context, p []byte, offset int64) (int, error)
	Close(ctx  context.Context) error
}

// Simplified interface for servers to implement files.
type Dirent interface {
	Qid() Qid

	// Entries, Walk, Create are only called if IsDir()
	Entries(ctx context.Context) ([]Dir, error)
	Walk(ctx context.Context, name string) (Dirent, error)
	// Note: Open will be called on the newly returned file
	// without any possibility for a client race.
	Create(ctx context.Context, name string, perm uint32) (Dirent, error)

	// Methods on file
	// Note: Open() is not called if IsDir()
	// An internal implementation calls Entries() instead.
	Open(ctx context.Context, mode Flag) (File, error)

	// Clone a copy of this dirent.
	// Generated when the session sees Walk with no arguments.
	Clone(ctx context.Context) (Dirent, error)

	// Note: If remove is called, the Dirent will no longer
	// be referenced by the server, but Clunk will not be called.
	Remove(ctx context.Context) error
	Clunk( ctx context.Context) error

	Stat( ctx context.Context) (Dir, error)
	WStat(ctx context.Context, stat Dir) error
}

// Helper function to check Dirent.Qid().Type for QTDIR bit.
func IsDir(d Dirent) bool {
	return d.Qid().Type & QTDIR != 0
}

type AuthFile interface {
	File		   // For read/write in auth protocols.
	Success() bool // Was the authentication successful?
}

type FServer interface {
	RequireAuth() bool
	Auth(ctx context.Context, uname, aname string) AuthFile
	Root(ctx context.Context, uname, aname string, af AuthFile) (Dirent, error)
}

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
	file File // non-nil if Open-ed
}

type authEnt struct {
	sync.Mutex
	afile AuthFile
    uname string
    aname string
}

type session struct {
	sync.Mutex
	fs	  FServer
	auths   map[Fid]*authEnt
	refs	map[Fid]*dirEnt
}

// Serve up the root Dirent.
// Fid-s are managed at this level.
//
//  For example, p9p/ufs translates all fid-s using sess.getRef(fid)
//  and https://9fans.github.io/usr/local/plan9/src/cmd/ramfs.c
//  uses user-defined structs for Fid-s.
func Serve(fs FServer) Handler {
	session := session{
		fs:	  fs,
		auths:   make(map[Fid]*authEnt),
		refs:	make(map[Fid]*dirEnt),
	}

	return Dispatch(&session)
}

// Acquires a lock on the dirEnt just after successful lookup.
// It is released before returning, unless `hodl` is true.
// In that case, a successful return means the caller is responsible
// for calling `ref.Unlock()`.
func (sess *session) getRef(fid Fid, hodl bool) (*dirEnt, error) {
	if fid == NOFID {
		return nil, ErrUnknownfid
	}

	sess.Lock()
	ref, found := sess.refs[fid]
	sess.Unlock()
	if !found {
		return nil, ErrUnknownfid
	}

	ref.Lock()
    if !hodl {
        ref.Unlock()
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

    sess.refs[fid] = &dirEnt{ ent: d }
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
//
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

    ref.Lock() // WARNING: We must ensure that no code acquires these
	sess.Lock() //         in reverse order.  
	delete(sess.refs, fid)
	sess.Unlock()
    ref.Unlock()

	// If the file has been opened, we also call Close()
	// TODO(frobnitzem): combine potential errors in return
	if ref.file != nil {
		ref.file.Close(ctx)
	}
	if remove {
		return ref.ent.Remove(ctx)
	} else {
		return ref.ent.Clunk(ctx)
	}
}

// Creates a new auth fid.
// These aref-s are locked until the afile is established.
func (sess *session) Auth(ctx context.Context, afid Fid,
						  uname, aname string) (Qid, error) {
	if afid == NOFID { // no-op
		return Qid{}, nil
	}
	if !sess.fs.RequireAuth() {
		return Qid{}, MessageRerror{Ename: "no auth"}
	}

	sess.Lock()
	_, found := sess.auths[afid]
    if found {
		sess.Unlock()
		return Qid{}, ErrDupfid
	}
    aref := &authEnt{uname: uname, aname: aname}
    aref.Lock()
    sess.auths[afid] = aref
	sess.Unlock()

	aref.afile = sess.fs.Auth(ctx, uname, aname)
    aref.Unlock()

	return Qid{}, nil
}

func (sess *session) Attach(ctx context.Context, fid, afid Fid,
							uname, aname string) (Qid, error) {
	// Not in the spec, but if we're to create files...
	//if uname == "" {
	//	return Qid{}, MessageRerror{Ename: "no user"}
	//}

	var aref *authEnt
    var af AuthFile

	// Auth was required. Check the AuthFile.
	if sess.fs.RequireAuth() {
		sess.Lock()
		var found bool
		aref, found = sess.auths[afid]
		if !found {
			sess.Unlock()
			return Qid{}, MessageRerror{Ename: "auth required"}
		}
		sess.Unlock()

        aref.Lock() // acquiring guarantees afile is present
        ok := aref.afile.Success()
        aref.Unlock()

		if !ok {
			return Qid{}, MessageRerror{Ename: "unauthorized"}
		}
        af = aref.afile
	}

    ref, err := sess.holdRef(fid)
	if err != nil {
		return Qid{}, err
    }

	ent, err := sess.fs.Root(ctx, uname, aname, af)
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
	var qids []Qid
	var ent Dirent // the newly discovered ent

    var newref *dirEnt

    // TODO(frobnitzem): use a list of ctx-s to
    // prevent close during any other action.
    // This involves modifying sess.getRef and sess.delRef
	ref, err := sess.getRef(fid, true)
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
        newref, err = sess.holdRef(fid)
        if err != nil {
            return nil, err
        }
    }

	// Both paths below must define ent and qids (or else return nil,err)
	if len(names) == 0 { // Clone
        if newfid == fid { // Walk is a no-op
            return append(qids, ent.Qid()), nil
        }
		var err error
		ent, err = ref.ent.Clone(ctx)
		if err != nil {
			return nil, err
		}
		qids = append(qids, ent.Qid())
	} else {
		var next Dirent
		var err error

		ent = ref.ent

		for _, name := range names {
			if !IsDir(ent) {
				err = MessageRerror{Ename: "not a directory"}
				break
			}
			next, err = ent.Walk(ctx, name)
			if err != nil {
				break
			}
			ent = next
			qids = append(qids, ent.Qid())
		}
		// An error walking the first path element gets propagated.
		if len(qids) == 0 {
			return qids, err
		}
	}

    if newfid == fid {
        // Re-use fid for result of walk.
        // Note: It is still locked.
        if ref.file != nil {
            // TODO(frobnitzem): note - ignoring error here
            ref.file.Close(ctx)
            ref.file = nil
        }
		ref.ent.Clunk(ctx)
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
	ref, err := sess.getRef(fid, true)
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
	ref, err := sess.getRef(fid, true)
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
	ref, err := sess.getRef(fid, true)
	if err != nil {
		return Qid{}, 0, err
	}

	return openLocked(ctx, ref, mode)
}

func openLocked(ctx context.Context, ref *dirEnt,
				mode Flag) (Qid, uint32, error) {
  	defer ref.Unlock()

	if ref.file != nil {
		return Qid{}, 0, MessageRerror{Ename: "already open"}
	}

	var file File

	if IsDir(ref.ent) {
		var err error
		var dirs []Dir
		dirs, err = ref.ent.Entries(ctx)
		if err != nil {
			return Qid{}, 0, err
		}
		file = NewFixedReaddir(NewCodec(), dirs)
	} else {
		var err error
		file, err = ref.ent.Open(ctx, mode)
		if err != nil {
			return Qid{}, 0, err
		}
	}

	ref.file = file
	iounit := uint32(0)
	return ref.ent.Qid(), iounit, nil
}

func (sess *session) Create(ctx context.Context, parent Fid, name string,
							perm uint32, mode Flag) (Qid, uint32, error) {
	var err error
	var ref *dirEnt
	var ent Dirent

	fail := func(name string) (Qid, uint32, error) {
        if ref != nil {  // Successful exit retains lock (calls openLocked)
            ref.Unlock() // so we only unlock on failure.
        }
		return Qid{}, 0, MessageRerror{Ename: name}
	}

	if name == "." || name == ".." {
		return fail("illegal filename")
	}

	ref, err = sess.getRef(parent, true)
	if err != nil {
		return fail(err.Error())
	}

	if !IsDir(ref.ent) {
		return fail("not a directory")
	}

	ent, err = ref.ent.Create(ctx, name, perm)
	if err != nil {
		return fail(err.Error())
	}

    // Success. Clean-up ref and replace with ent.
	if ref.file != nil {
		ref.file.Close(ctx)
        ref.file = nil
	}
	ref.ent.Clunk(ctx)
	ref.ent = ent

	return openLocked(ctx, ref, mode)
}

func (sess *session) Stat(ctx context.Context, fid Fid) (Dir, error) {
	ref, err := sess.getRef(fid, true)
	if err != nil {
		return Dir{}, err
	}
    defer ref.Unlock()

	return ref.ent.Stat(ctx)
}

func (sess *session) WStat(ctx context.Context, fid Fid, dir Dir) error {
	ref, err := sess.getRef(fid, true)
	if err != nil {
		return err
	}
    defer ref.Unlock()

	return ref.ent.WStat(ctx, dir)
}

func (sess *session) Version() (msize int, version string) {
	return DefaultMSize, DefaultVersion
}
