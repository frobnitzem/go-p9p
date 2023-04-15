package sleepfs

import (
	"fmt"

	p9p "github.com/frobnitzem/go-p9p"
)

func (s sleepTime) IsDir() bool {
	return len(s) < 2
}

func (s sleepTime) Qid() p9p.Qid {
	q := p9p.Qid{Version: 0}
	if s.IsDir() {
		if len(s) != 0 { // dirs are all even
			q.Path = 2 + uint64(s[0])*2
		} // root is 0
		q.Type |= p9p.QTDIR
	} else { // files are all odd
		if len(s) == 2 {
			q.Path = (uint64(s[0])*1000+uint64(s[1]))*2 + 1
		}
	}
	return q
}

func (st sleepTime) Info() p9p.Dir {
	dir := p9p.Dir{Qid: st.Qid()}

	if len(st) > 0 {
		dir.Name = fmt.Sprintf("%d", st[len(st)-1])
	} else {
		dir.Name = "/"
	}
	dir.Length = 0
	dir.MUID = "sleeper"

	if st.IsDir() {
		dir.Qid.Type |= p9p.QTDIR
		dir.Mode |= p9p.DMDIR
	}

	return dir
}
