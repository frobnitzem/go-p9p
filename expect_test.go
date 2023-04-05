package p9p

import (
    "testing"
    "net"
    "time"
    "sync"
    "github.com/stretchr/testify/assert"
    "golang.org/x/net/context"
)

func TestPipe(t *testing.T) {
    var wg sync.WaitGroup
    assert := assert.New(t)

    req, rep := net.Pipe()
    end := time.Now().Add(time.Second)
    req.SetDeadline(end)
    rep.SetDeadline(end)

    msg  := []byte("GET / HTTP/1.0\r\n\r\n")
    wg.Add(2)
    go func() {
        defer wg.Done()
        n, err := req.Write(msg)
        assert.Nil(err)
        assert.Equal(n, len(msg))
    }()
    go func() {
        defer wg.Done()
        ans := make([]byte, 100)
        m, err := rep.Read(ans)
        ans = ans[:m]
        assert.Nil(err)
        assert.Equal(len(msg), m)
        assert.Equal(msg, ans)
    }()
    wg.Wait()
}

/** Mimick the server and test the client's
 *  send/recv conversation.
 */
func TestClient(t *testing.T) {
    var wg sync.WaitGroup
    assert := assert.New(t)

	ctx := context.Background()
    ctx, _ = context.WithTimeout(ctx, 1*time.Second)

    reqC, repC := net.Pipe()
    //end := time.Now().Add(time.Second)
    //reqC.SetDeadline(end)
    //repC.SetDeadline(end)

    wg.Add(2)
    go func() {
        defer wg.Done()
        session, err := NewSession(ctx, reqC)
        assert.Nil(err)
        msize, version := session.Version()
        assert.Equal(1024, msize)
        assert.Equal("9P2000", version)

        // This results in an error, since the server never reads it.
        _, err = session.Attach(ctx, 0, NOFID, "user1", "/")
        assert.NotNil(err)
        //session.Walk()
    }()
    go func() {
        defer wg.Done()
        srv := NewChannel(repC, 1024)
        assert.Equal(1024, srv.MSize())
        //srv.SetMSize()

        // version negotiation
        inp := new(Fcall)
        assert.Nil( srv.ReadFcall(ctx, inp) )
        tver, ok := inp.Message.(MessageTversion)
        assert.True(ok)
        assert.True(tver.MSize > 128)
        assert.Equal(tver.Version, "9P2000")

        out := newFcall(NOTAG, MessageRversion{
            Version: "9P2000",
            MSize: 1024,
        })
        assert.Nil( srv.WriteFcall(ctx, out) )
    }()
    wg.Wait()
}
