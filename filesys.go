package p9p

import "context"

// Note: Like information from all ent/file inputs, the server
// should copy details from the AuthFile if you want
// to retain them internally past the lifetime of this function call.
//
// The client, on the other hand, uniquely owns the function outputs.
//
// RequireAuth is not able to be determined reliably by the client,
// which will always return false without asking the server.
type FileSys interface {
	RequireAuth(ctx context.Context) bool
	Auth(ctx context.Context, uname, aname string) (AuthFile, error)
	Attach(ctx context.Context, uname, aname string, af AuthFile) (Dirent, error)
}

type AuthFile interface {
	File // For read/write in auth protocols.
	Close(ctx context.Context) error
	Success() bool // Was the authentication successful?
}

// FileSystem implementions gather more data than the required
// minimal information to send messages.  The following expanded
// interfaces are generated and used internally.
// They contain useful information that can be looked up
// from the server, but are only guaranteed to be active while
// an active call is running on the server.
type ExpandedAuth interface {
	AuthFile
	ExpandedEnt
	Opened() (file ExpandedFile, Mode uint32)
}

type ExpandedFile interface {
	File
	ExpandedEnt
	Mode() (Mode uint32)
	// TODO(frobnitzem): also implement a max-IOUnit here.
}

type ExpandedEnt interface {
	Dirent
	Path() string
	// file is nil if ent is not yet successfully opened.
	Opened() (file ExpandedFile, Mode uint32)
}

// Simplified interface to a file that has been Open-ed.
// Note: Since a Dirent can only be opened once,
// it is up to Clunk to close any underlying File state,
// Including remove on close (ORCLOSE flag)
// FileSys won't call it automatically.
type File interface {
	Read(ctx context.Context, p []byte, offset int64) (int, error)
	Write(ctx context.Context, p []byte, offset int64) (int, error)

	// IOUnit must be >0 on the client side (unless the File is invalid)
	// May be 0 on the server side.
	// Must not be <0.
	IOUnit() int
}

// Simplified interface for servers to implement files.
type Dirent interface {
	Qid() Qid

	// OpenDir, Walk, Create are only called if IsDir()
	// Note: IsDir iff. Qid.Type & p9p.QTDIR != 0
	OpenDir(ctx context.Context) (ReadNext, error)

	// Walk is guaranteed not to see '.' or have paths containing '/'
	// NOTE(frobnitzem): we could expand Walk to take a bool
	// indicating whether to re-use the same Fid.
	// For now, the client assumes it will make a new Fid,
	// and the server honors the client's choice either way.
	Walk(ctx context.Context, name ...string) ([]Qid, Dirent, error)
	// Note: Open is not called after create.
	// The server must create and open together.
	//
	// If the file represents a Dir, then the file returned
	// is discarded and replaced with a nice directory entry reader.
	Create(ctx context.Context, name string,
		perm uint32, mode Flag) (Dirent, File, error)

	// Methods on file
	// Note: Open() is not called if IsDir()
	// An internal implementation calls OpenDir() instead.
	Open(ctx context.Context, mode Flag) (File, error)

	// Note: If remove is called, the Dirent will no longer
	// be referenced by the server, but Clunk will not be called.
	// If remove is called on an open file, it is responsible
	// for releasing server resources associated with the file.
	Remove(ctx context.Context) error
	// If Clunk is called on an open file, it is responsible
	// for releasing server resources associated with the file.
	// It is also responsible for implementing actions like ORCLOSE.
	Clunk(ctx context.Context) error

	Stat(ctx context.Context) (Dir, error)
	WStat(ctx context.Context, stat Dir) error
}

// Helper function to check Dirent.Qid().Type for QTDIR bit.
func IsDir(d Dirent) bool {
	return d.Qid().Type&QTDIR != 0
}
