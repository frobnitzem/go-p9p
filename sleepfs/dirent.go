package sleepfs

import (
	"context"
	"strconv"
	"time"

	p9p "github.com/frobnitzem/go-p9p"
)

type dirList struct {
	s   sleepTime
	cur int
	max int
}

func (d *dirList) Next(ctx context.Context) ([]p9p.Dir, error) {
	if d.cur == d.max {
		return nil, nil
	}
	// FIXME: actually return some Dir-s
	return nil, nil
}

func (s sleepTime) OpenDir(ctx context.Context) (p9p.ReadNext, error) {
	if len(s) >= 2 {
		return nil, p9p.MessageRerror{Ename: "not a directory"}
	}

	if len(s) == 0 {
		return (&dirList{s, 0, 5}).Next, nil
	}
	return (&dirList{s, 0, 1000}).Next, nil
}

func (s sleepTime) Clunk(ctx context.Context) error {
	return nil
}

func (s sleepTime) Remove(ctx context.Context) error {
	return p9p.ErrNoremove
}

func (s sleepTime) Walk(ctx context.Context, names ...string) ([]p9p.Qid, p9p.Dirent, error) {
	if len(names) == 0 {
		return nil, s, nil
	}

	// Path is guaranteed canonical.
	// First walk .. (guarding against running out of len(s))
	qids := make([]p9p.Qid, len(names))
	i := 0
	for ; len(names) > 0 && names[0] == ".."; i++ {
		if len(s) == 0 {
			return qids, nil, nil
		}
		names = names[1:]
		qids[i] = sleepTime(s[:len(s)-1-i]).Qid()
	}
	s = s[:len(s)-i]
	// Next walk forward (ensuring valid file names)
	for ; len(names) > 0; i++ {
		n, err := strconv.Atoi(names[0])
		if err != nil {
			return nil, nil, err
		}
		if n < 0 || n >= 1000 || (len(s) == 0 && n >= 5) {
			if len(qids) == 0 {
				err = p9p.MessageRerror{Ename: "no such file"}
			}
			return qids, nil, err
		}

		s = append(s, uint(n))
		names = names[1:]
		qids[i] = s.Qid()
	}

	return qids, s, nil
}

func (s sleepTime) Create(ctx context.Context, name string,
	perm uint32, mode p9p.Flag) (p9p.Dirent, p9p.File, error) {
	return nil, nil, p9p.ErrNocreate
}

func (s sleepTime) Stat(ctx context.Context) (p9p.Dir, error) {
	return s.Info(), nil
}

func (s sleepTime) WStat(ctx context.Context, dir p9p.Dir) error {
	return p9p.ErrNowstat
}

func (s sleepTime) Open(ctx context.Context, mode p9p.Flag) (p9p.File, error) {
	return s, nil
}

func (s sleepTime) duration() time.Duration {
	t := 0 * time.Second
	if len(s) > 0 {
		t += time.Duration(s[0]) * time.Second
	}
	if len(s) > 1 {
		t += time.Duration(s[1]) * time.Millisecond
	}
	if len(s) > 2 {
		t += time.Duration(s[2]) * time.Microsecond
	}
	return t
}

func (s sleepTime) Read(ctx context.Context, p []byte,
	offset int64) (n int, err error) {
	time.Sleep(s.duration())
	return 0, nil
}

func (s sleepTime) Write(ctx context.Context, p []byte,
	offset int64) (n int, err error) {
	time.Sleep(s.duration())
	return len(p), nil
}

func (s sleepTime) Close(ctx context.Context) error {
	return nil
}

func (_ sleepTime) IOUnit() int {
	return 0
}
