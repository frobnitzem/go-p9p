package p9p

import (
	"context"
	"log"
	"os"
)

type logging struct {
	session Session
	logger  log.Logger
}

var _ Session = &logging{}

// Wrap a Session, producing log messages to os.Stdout
// whenever Auth, Attach, Remove, and Stop are called.
func NewLogger(prefix string, session Session) Session {
	return &logging{
		session: session,
		logger:  *log.New(os.Stdout, prefix, 0),
	}
}

func (l *logging) Stop(err error) (err2 error) {
	err2 = l.session.Stop(err)
	l.logger.Printf("Stop(%v) -> %v", err, err2)
	return
}
func (l *logging) Auth(ctx context.Context, afid Fid, uname, aname string) (qid Qid, err error) {
	qid, err = l.session.Auth(ctx, afid, uname, aname)
	l.logger.Printf("Auth(%v, %s, %s) -> (%v, %v)", afid, uname, aname, qid, err)
	return
}

func (l *logging) Attach(ctx context.Context, fid, afid Fid, uname, aname string) (qid Qid, err error) {
	qid, err = l.session.Attach(ctx, fid, afid, uname, aname)
	l.logger.Printf("Attach(%v, %v, %s, %s) -> (%v, %v)", fid, afid, uname, aname, qid, err)
	return
}

func (l *logging) Clunk(ctx context.Context, fid Fid) (err error) {
	err = l.session.Clunk(ctx, fid)
	l.logger.Printf("Clunk(%v) -> %v", fid, err)
	return
}

func (l *logging) Remove(ctx context.Context, fid Fid) (err error) {
	err = l.session.Remove(ctx, fid)
	l.logger.Printf("Remove(%v) -> %v", fid, err)
	return
}

func (l *logging) Walk(ctx context.Context, fid Fid, newfid Fid, names ...string) (qids []Qid, err error) {
	qids, err = l.session.Walk(ctx, fid, newfid, names...)
	l.logger.Printf("Walk(%v, %v, %v) -> %v, %v", fid, newfid, names, qids, err)
	return
}

func (l *logging) Read(ctx context.Context, fid Fid, p []byte, offset int64) (n int, err error) {
	n, err = l.session.Read(ctx, fid, p, offset)
	l.logger.Printf("Read(%v, [%d], %v) -> %v, %v", fid, len(p), offset, n, err)
	return n, err
}

func (l *logging) Write(ctx context.Context, fid Fid, p []byte, offset int64) (n int, err error) {
	n, err = l.session.Write(ctx, fid, p, offset)
	l.logger.Printf("Write(%v, [%d], %v) -> %v, %v", fid, len(p), offset, n, err)
	return
}

func (l *logging) Open(ctx context.Context, fid Fid, mode Flag) (qid Qid, msize uint32, err error) {
	qid, msize, err = l.session.Open(ctx, fid, mode)
	l.logger.Printf("Open(%v, %x) -> %v, %v, %v", fid, mode, qid, msize, err)
	return
}

func (l *logging) Create(ctx context.Context, parent Fid, name string, perm uint32, mode Flag) (qid Qid, msize uint32, err error) {
	qid, msize, err = l.session.Create(ctx, parent, name, perm, mode)
	l.logger.Printf("Create(%v, %v, %x, %x) -> %v, %v, %v", parent, name, perm, mode, qid, msize, err)
	return
}

func (l *logging) Stat(ctx context.Context, fid Fid) (dir Dir, err error) {
	dir, err = l.session.Stat(ctx, fid)
	l.logger.Printf("Stat(%v) -> %v, %v", fid, dir, err)
	return
}

func (l *logging) WStat(ctx context.Context, fid Fid, dir Dir) (err error) {
	err = l.session.WStat(ctx, fid, dir)
	l.logger.Printf("WStat(%v, %v) -> %v", fid, dir, err)
	return
}

// TODO(frobnitzem): make version take int32 and string too...
// func (l *logging) Version(msize int32, version string) (int, string) {
func (l *logging) Version() (msize int, ver string) {
	msize, ver = l.session.Version()
	l.logger.Printf("Version() -> %v, %v", msize, ver)
	return
}
