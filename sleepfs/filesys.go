package sleepfs

import (
	"context"

	"github.com/frobnitzem/go-p9p"
)

type sServer struct{}
type sleepTime []uint // sec, msec

func NewServer(ctx context.Context) p9p.FileSys {
	return sServer{}
}

func (_ sServer) RequireAuth(_ context.Context) bool {
	return false
}
func (_ sServer) Auth(ctx context.Context,
	uname, aname string) (p9p.AuthFile, error) {
	return nil, nil
}
func (_ sServer) Attach(ctx context.Context, uname, aname string,
	af p9p.AuthFile) (p9p.Dirent, error) {
	return sleepTime{}, nil
}
