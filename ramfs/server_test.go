package ramfs

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/frobnitzem/go-p9p"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

/** Mimick the client and test the server's behavior.
 */
func TestServer(t *testing.T) {
	var wg sync.WaitGroup
	assert := assert.New(t)

	ctx := context.Background()
	sctx, cancel := context.WithTimeout(ctx, 1*time.Second)

	reqC, repC := net.Pipe()
	//end := time.Now().Add(time.Second)
	//reqC.SetDeadline(end)
	//repC.SetDeadline(end)

	// Note: nanoseconds are not encoded in file times
	theTime := time.Unix(112321, 0).UTC()
	theDir := p9p.Dir{AccessTime: theTime, ModTime: theTime}
	wg.Add(1)
	go func() {
		defer wg.Done()

		srv := NewServer(sctx)
		session := p9p.SFileSys(srv)

		msize, version := session.Version()
		assert.Equal("9P2000", version)
		assert.True(msize >= 1024)

		err := p9p.ServeConn(sctx, repC, p9p.SSession(session))
		assert.NotNil(err)
		assert.Equal(err.Error(), "context canceled")
	}()

	var count int

	session, err := p9p.CSession(ctx, reqC)
	assert.Nil(err)

	msize, version := session.Version()
	assert.Equal("9P2000", version)
	assert.True(msize >= 1024)

	fid0 := p9p.Fid(0)
	fid1 := p9p.Fid(1)
	//fid7 := p9p.Fid(7)
	fid100 := p9p.Fid(100)

	_, err = session.Auth(ctx, fid0, "user1", "/")
	assert.NotNil(err)

	qid, err := session.Auth(ctx, p9p.NOFID, "", "")
	assert.Nil(err)
	if err != nil {
		assert.True(qid.Type&p9p.QTAUTH != 0)
	}
	_, err = session.Attach(ctx, fid0, fid100, "user1", "/")
	assert.NotNil(err)

	qid, err = session.Attach(ctx, fid0, p9p.NOFID, "user1", "/")
	assert.Nil(err)
	if err != nil {
		assert.True(qid.Type&p9p.QTDIR != 0)
	}

	_, err = session.Walk(ctx, fid0, fid1, "a_file_that_does_not_exist")
	_, ok := err.(p9p.MessageRerror)
	assert.True(ok)

	err = session.Clunk(ctx, fid1) // should not exist
	_, ok = err.(p9p.MessageRerror)
	assert.True(ok)

	qids, err := session.Walk(ctx, fid0, fid1) // Clone
	assert.Nil(err)
	assert.True(len(qids) == 0)
	if len(qids) == 1 {
		assert.True(qids[0].Type&p9p.QTDIR != 0, "fid1 is also a dir.")
	}

	err = session.Remove(ctx, fid1)
	assert.Equal("9p: cannot remove root", err.Error())

	qids, err = session.Walk(ctx, fid0, fid1) // Clone
	assert.Nil(err)
	assert.True(len(qids) == 0)
	if len(qids) == 1 {
		assert.True(qids[0].Type&p9p.QTDIR != 0, "fid1 is also a dir.")
	}

	_, _, err = session.Create(ctx, fid1, "a_new_test_file", 0644, p9p.ORDWR)
	// Qid, IOUnit, err
	assert.Nil(err)

	_, _, err = session.Open(ctx, fid1, p9p.ORDWR)
	// Qid, IOUnit, err
	_, ok = err.(p9p.MessageRerror)
	assert.True(ok, "Create also opens.")

	count, err = session.Write(ctx, fid1, []byte("abcd"), 0)
	assert.Nil(err)
	assert.Equal(4, count)
	msg := make([]byte, 100)
	count, err = session.Read(ctx, fid1, msg, 1)
	assert.Nil(err)
	assert.Equal(3, count)
	assert.Equal([]byte("bcd"), msg[:3])

	session.WStat(ctx, fid1, theDir) // ignoring returned error
	//assert.Nil(err)

	_, err = session.Stat(ctx, fid1) // Dir, error
	assert.Nil(err)
	err = session.Remove(ctx, fid1)
	assert.Nil(err)

	err = session.Clunk(ctx, fid0)
	assert.Nil(err)

	cancel() // signal the server to stop serving
	wg.Wait()
}

/* TODO: capture mkdir/walk session testing walk to ..
mkdir a/b
cd a
cd ..
cd a/b
cd ../..
cd a/b
mkdir ../c
ls ..
cd ../c

walk:  [..] [] 0 0 [qid(dir, v=0, p=1)]
walk:  [.. ..] [] 0 0 [qid(file, v=0, p=0) qid(dir, v=0, p=1)]
walk:  [..] [a] 0 1 [qid(dir, v=0, p=2)]
walk:  [..] [a] 0 1 [qid(dir, v=0, p=2)]
walk:  [.. c] [a c] 1 1 [qid(dir, v=0, p=2) qid(dir, v=0, p=4)]
*/
