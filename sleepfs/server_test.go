package sleepfs

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

		// TODO(frobnitzem): implement options here by trying to
		// force the msize to 1024 for demonstration purposes.
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

	_, err = session.Auth(ctx, fid0, "undercover", "/")
	assert.NotNil(err)

	qid, err := session.Auth(ctx, p9p.NOFID, "", "")
	assert.Nil(err)
	if err != nil {
		assert.True(qid.Type&p9p.QTAUTH != 0)
	}
	_, err = session.Attach(ctx, fid0, fid100, "sleepy", "/")
	assert.NotNil(err)

	qid, err = session.Attach(ctx, fid0, p9p.NOFID, "snooz", "/")
	assert.Nil(err)
	if err != nil {
		assert.True(qid.Type&p9p.QTDIR != 0)
	}

	_, err = session.Walk(ctx, fid0, fid1, "x")
	assert.NotNil(err)

	_, err = session.Walk(ctx, fid0, fid1, "-1")
	assert.NotNil(err)

	_, err = session.Walk(ctx, fid0, fid1, "10")
	assert.NotNil(err)
	//_, ok := err.(p9p.MessageRerror)
	//assert.True(ok)

	err = session.Clunk(ctx, fid1) // should not exist
	_, ok := err.(p9p.MessageRerror)
	assert.True(ok)

	qids, err := session.Walk(ctx, fid0, fid1, "0")
	assert.Nil(err)
	assert.Equal(1, len(qids))

	_, _, err = session.Create(ctx, fid1, "a_new_test_file", 0644, p9p.ORDWR)
	// Qid, IOUnit, err
	assert.NotNil(err)

	qids, err = session.Walk(ctx, fid1, fid100, "10")
	assert.Nil(err)
	assert.Equal(1, len(qids))
	if len(qids) == 1 {
		assert.True(qids[0].Type&p9p.QTDIR == 0, "fid100 is not a dir.")
	}

	qid2, _, err := session.Open(ctx, fid100, p9p.OREAD)
	// Qid, IOUnit, err
	assert.Equal(qids[0], qid2)

	count, err = session.Write(ctx, fid100, []byte("abcd"), 0)
	assert.NotNil(err)

	msg := make([]byte, 1024)
	count, err = session.Read(ctx, fid100, msg, 1)
	//assert.Nil(err)
	assert.Equal(0, count)

	err = session.WStat(ctx, fid100, theDir) // ignoring returned error
	assert.NotNil(err)

	_, err = session.Stat(ctx, fid100) // Dir, error
	assert.Nil(err)
	err = session.Remove(ctx, fid100)
	assert.NotNil(err)

	err = session.Clunk(ctx, fid1)
	assert.Nil(err)

	cancel() // signal the server to stop serving
	wg.Wait()
}
