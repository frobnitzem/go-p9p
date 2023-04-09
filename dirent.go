package p9p

import "context"

// Simplified interface to a file that has been Open-ed.
type File interface {
	Read(ctx context.Context, p []byte, offset int64) (int, error)
	Write(ctx context.Context, p []byte, offset int64) (int, error)
    // Note: it is up to Close to implement remove on close (ORCLOSE)
    // FServer won't call it automatically.
	Close(ctx context.Context) error
}

// Simplified interface for servers to implement files.
type Dirent interface {
	Qid() Qid

	// Entries, Walk, Create are only called if IsDir()
	// Note: IsDir iff. Qid.Type & p9p.QTDIR != 0
	Entries(ctx context.Context) ([]Dir, error)
	// Walk is guaranteed not to see '.' or have paths containing '/'
	Walk(ctx context.Context, name string) (Dirent, error)
	// Note: Open will be called on the newly returned file
	// without any possibility for a client race.
	Create(ctx context.Context, name string,
           perm uint32, mode Flag) (Dirent, error)

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
	Clunk(ctx context.Context) error

	Stat(ctx context.Context) (Dir, error)
	WStat(ctx context.Context, stat Dir) error
}

// Helper function to check Dirent.Qid().Type for QTDIR bit.
func IsDir(d Dirent) bool {
	return d.Qid().Type&QTDIR != 0
}
