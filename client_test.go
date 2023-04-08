package p9p

import (
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"net"
	"sync"
	"testing"
	"time"
)

func TestPipe(t *testing.T) {
	var wg sync.WaitGroup
	assert := assert.New(t)

	req, rep := net.Pipe()
	end := time.Now().Add(time.Second)
	req.SetDeadline(end)
	rep.SetDeadline(end)

	msg := []byte("GET / HTTP/1.0\r\n\r\n")
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

type ExpectReply func(inp Message) Message

/** Mimick the server and test the client's
 *  send/recv conversation.
 */
func TestClient(t *testing.T) {
	var wg sync.WaitGroup
	assert := assert.New(t)

	ctx := context.Background()
	ctx, _ = context.WithTimeout(ctx, 5*time.Second)

	reqC, repC := net.Pipe()
	//end := time.Now().Add(time.Second)
	//reqC.SetDeadline(end)
	//repC.SetDeadline(end)

	// Note: nanoseconds are not encoded in file times
	theTime := time.Unix(112321, 0).UTC()
	theDir := Dir{AccessTime: theTime,
		ModTime: theTime}
	wg.Add(2)
	go func() {
		defer wg.Done()
		session, err := NewSession(ctx, reqC)
		assert.Nil(err)
		msize, version := session.Version()
		assert.Equal(1024, msize)
		assert.Equal("9P2000", version)

		var count int

		_, err = session.Auth(ctx, Fid(100), "user1", "/")
		assert.Nil(err)
		_, err = session.Attach(ctx, Fid(0), Fid(100), "user1", "/")
		assert.Nil(err)
		_, err = session.Walk(ctx, Fid(0), Fid(1), "blah")
		// []Qid, error
		assert.Nil(err)
		err = session.Clunk(ctx, Fid(0))
		assert.Nil(err)

		_, _, err = session.Create(ctx, Fid(1), "file1", 0644, ORDWR)
		// Qid, IOUnit, err
		assert.Nil(err)

		// Opening 0 would have made sense here, had we not clunked it
		// but this is a nonsense-test anyway.
		_, _, err = session.Open(ctx, Fid(7), OREAD)
		// Qid, IOUnit, err
		assert.Nil(err)

		count, err = session.Write(ctx, Fid(1), []byte("abcd"), 0)
		assert.Nil(err)
		assert.Equal(4, count)
		msg := make([]byte, 100)
		count, err = session.Read(ctx, Fid(1), msg, 1)
		assert.Nil(err)
		assert.Equal(3, count)
		assert.Equal([]byte("bcd"), msg[:3])

		err = session.WStat(ctx, Fid(1), theDir)
		assert.Nil(err)
		_, err = session.Stat(ctx, Fid(1)) // Dir, error
		assert.Nil(err)
		err = session.Remove(ctx, Fid(1))
		assert.Nil(err)
	}()
	go func() {
		defer wg.Done()
		srv := NewChannel(repC, 1024)
		assert.Equal(1024, srv.MSize())
		//srv.SetMSize()

		for _, step := range []ExpectReply{
			// version negotiation
			func(inp Message) Message {
				tver, ok := inp.(MessageTversion)
				assert.True(ok)
				assert.True(tver.MSize > 128)
				assert.Equal(tver.Version, "9P2000")

				return MessageRversion{
					Version: "9P2000",
					MSize:   1024,
				}
			},
			// auth
			func(inp Message) Message {
				att, ok := inp.(MessageTauth)
				assert.True(ok)
				assert.Equal(Fid(100), att.Afid)
				assert.Equal("user1", att.Uname)
				assert.Equal("/", att.Aname)

				return MessageRauth{
					Qid: Qid{Type: QTDIR, Version: 0, Path: 100},
				}
			},
			// attach
			func(inp Message) Message {
				att, ok := inp.(MessageTattach)
				assert.True(ok)
				assert.Equal(Fid(0), att.Fid)
				assert.Equal(Fid(100), att.Afid)
				assert.Equal("user1", att.Uname)
				assert.Equal("/", att.Aname)

				return MessageRattach{
					Qid: Qid{Type: QTDIR, Version: 0, Path: 0},
				}
			},
			// walk
			func(inp Message) Message {
				msg, ok := inp.(MessageTwalk)
				assert.True(ok)
				assert.Equal(Fid(0), msg.Fid)
				assert.Equal(Fid(1), msg.Newfid)
				assert.Equal([]string{"blah"}, msg.Wnames)

				return MessageRwalk{
					Qids: []Qid{Qid{Type: QTDIR, Version: 0, Path: 1}},
				}
			},
			// clunk
			func(inp Message) Message {
				msg, ok := inp.(MessageTclunk)
				assert.True(ok)
				assert.Equal(Fid(0), msg.Fid)

				return MessageRclunk{}
			},
			// create
			func(inp Message) Message {
				msg, ok := inp.(MessageTcreate)
				assert.True(ok)
				assert.Equal(Fid(1), msg.Fid)
				assert.Equal("file1", msg.Name)
				assert.Equal(uint32(0644), msg.Perm)
				assert.Equal(ORDWR, msg.Mode)

				return MessageRcreate{
					Qid:    Qid{Type: QTFILE, Version: 0, Path: 2},
					IOUnit: 1024,
				}
			},
			// open
			func(inp Message) Message {
				msg, ok := inp.(MessageTopen)
				assert.True(ok)
				assert.Equal(Fid(7), msg.Fid)
				assert.Equal(OREAD, msg.Mode)

				return MessageRopen{
					Qid:    Qid{Type: QTDIR, Version: 0, Path: 7},
					IOUnit: 1024,
				}
			},
			// write
			func(inp Message) Message {
				msg, ok := inp.(MessageTwrite)
				assert.True(ok)
				assert.Equal(Fid(1), msg.Fid)
				assert.Equal(uint64(0), msg.Offset)
				assert.Equal([]byte("abcd"), msg.Data)

				return MessageRwrite{
					Count: 4,
				}
			},
			// read
			func(inp Message) Message {
				msg, ok := inp.(MessageTread)
				assert.True(ok)
				assert.Equal(Fid(1), msg.Fid)
				assert.Equal(uint64(1), msg.Offset)
				assert.Equal(uint32(100), msg.Count)

				return MessageRread{
					Data: []byte("bcd"),
				}
			},
			// wstat
			func(inp Message) Message {
				msg, ok := inp.(MessageTwstat)
				assert.True(ok)
				assert.Equal(Fid(1), msg.Fid)
				assert.Equal(theDir, msg.Stat)

				return MessageRwstat{}
			},
			// stat
			func(inp Message) Message {
				msg, ok := inp.(MessageTstat)
				assert.True(ok)
				assert.Equal(Fid(1), msg.Fid)

				return MessageRstat{
					Stat: theDir,
				}
			},
			// remove
			func(inp Message) Message {
				msg, ok := inp.(MessageTremove)
				assert.True(ok)
				assert.Equal(Fid(1), msg.Fid)

				return MessageRremove{}
			},
		} {
			inp := new(Fcall)
			assert.Nil(srv.ReadFcall(ctx, inp))
			msg := step(inp.Message)
			out := newFcall(inp.Tag, msg)
			assert.Nil(srv.WriteFcall(ctx, out))
		}
		// end expect-reply loop
	}()
	wg.Wait()
}
