package ufs

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
    "github.com/frobnitzem/go-p9p"
)

/** Mimick the client and test the server's behavior.
 */
func TestServer(t *testing.T) {
	assert := assert.New(t)

	ctx := context.Background()
	ctx, _ = context.WithTimeout(ctx, 1*time.Second)

	reqC, repC := net.Pipe()
	//end := time.Now().Add(time.Second)
	//reqC.SetDeadline(end)
	//repC.SetDeadline(end)

    // Note: nanoseconds are not encoded in file times
	theTime := time.Unix(112321, 0).UTC()
	theDir := p9p.Dir{AccessTime: theTime, ModTime: theTime}
	go func() {
        session, err := NewSession(ctx, "/tmp")
        assert.Nil(err)

        err = p9p.ServeConn(ctx, repC, p9p.Dispatch(session))
        assert.Nil(err)

		msize, version := session.Version()
		assert.Equal("9P2000", version)
		assert.True(msize >= 1024)
	}()

	var count int

    session, err := p9p.NewSession(ctx, reqC)
    assert.Nil(err)

	msize, version := session.Version()
	assert.Equal("9P2000", version)
	assert.True(msize >= 1024)

    fid0 := p9p.Fid(0)
    fid1 := p9p.Fid(1)
    //fid7 := p9p.Fid(7)
    fid100 := p9p.Fid(100)

	_, err = session.Auth(ctx, fid0, "user1", "/")
	assert.Nil(err)
	_, err = session.Attach(ctx, fid0, fid100, "user1", "/")
	assert.Nil(err)

	_, err = session.Walk(ctx, fid0, fid1, "a_file_that_does_not_exist")
    _, ok := err.(p9p.MessageRerror)
    assert.True(ok)

	err = session.Clunk(ctx, fid1)
    _, ok = err.(p9p.MessageRerror)
    assert.True(ok)

	_, err = session.Walk(ctx, fid0, fid1)
    assert.Nil(err)

	_, _, err = session.Create(ctx, fid1, "a_new_test_file", 0644, p9p.ORDWR)
	// Qid, IOUnit, err
	assert.Nil(err)

	_, _, err = session.Open(ctx, fid1, p9p.ORDWR)
	// Qid, IOUnit, err
	assert.Nil(err)
    //_, ok = err.(p9p.MessageRerror)
    //assert.True(ok, "Open fails on a newly created file?")

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
}
