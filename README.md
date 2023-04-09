# p9p

[![GoDoc](https://godoc.org/github.com/frobnitzem/go-p9p?status.svg)](https://godoc.org/github.com/frobnitzem/go-p9p) [![Apache licensed](https://img.shields.io/badge/license-Apache-blue.svg)](https://raw.githubusercontent.com/frobnitzem/go-p9p/master/LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/frobnitzem/go-p9p)](https://goreportcard.com/report/github.com/frobnitzem/go-p9p) ![CI Test](https://github.com/frobnitzem/go-p9p/actions/workflows/go.yml/badge.svg)
---

A modern, performant 9P library for Go.

For API documentation, see the
[GoDoc](https://godoc.org/github.com/frobnitzem/go-p9p).

Refer to [9P's documentation](http://9p.cat-v.org/documentation) for more details on the protocol.

## Example

Download the package in the usual way:

    go get github.com/frobnitzem/go-p9p

Now run the server and client.  Since there's no authentication,
it's safest to put it into a unix socket.

    cd $HOME/go/frobnitzem/go-p9p
    go run cmd/9ps/main.go -root $HOME/src -addr unix:/tmp/sock9 &
    chmod 700 /tmp/sock9
    go run cmd/9pr/main.go -addr unix:/tmp/sock9

You should get a prompt,

    / 🐳 >

There's actually a tab-autocomplete to show you commands
to run.

Bring the server down with:

    kill %
    rm /tmp/sock9


## Build your own filesystem

TODO: write a description here...

## Protocol Stack

This package handles the 9P protocol by implementing a stack of layers.
For the server, each layer does some incremental translation of the
messages "on the wire" into program state.  The client is opposite,
translating function calls into wire representations, and invoking
client callbacks with responses from the server.

Applications can pick one of the layers to interact with.
Lower layers generally require more work on the applications side
(tracking state, encoding messages, etc.).
It's generally recommended to interface only with the uppermost
layer's API.

Extending the layers upward to make even simpler interface API-s,
as well as implementing proper authentication
and session encryption is on the TODO list.


### Server Stack

servefs.go: FSession(fs FServer) Session
  - Creates a Session type from an FServer
  - The FServer represents a fileserver broken into 3 levels,
    AuthFile-s, Dirent-s, and Files
  - Rather than track Fid-s, calls to Dirent-s create
    more Dirent and Files.

dispatcher.go: `Dispatch(session Session) Handler`
  - Delegates each type of messages.go:Message to a
    function call (from Session).
  - Tags and TFlush-s are handled at this level,
    so higher levels do not see them.  Instead, they see 
    context.Context objects to indicate potential cancellation.

serveconn.go: `ServeConn(ctx context.Context, cn net.Conn, handler Handler) error`
  - Negotiates protocol (with a timeout of 1 second).
  - Calls server loop, reading messages and sending them to the handler.
  - Version messages are handled at this level.  They
    have the effect of deleting the current session
    and starting a new one. (Note: Check this to be sure.)

serverconn.go: `(c *conn) serve() error`
  - Server loop, strips Tags and TFlush messages
  - details:
      - runs reader and writer goroutines
      - maps from Tags to activeRequest structures
      - spawns a goroutine to call c.handler.Handle on every non-TFlush
        - these handlers get new contexts
      - for TFlush, cancels the corresponding call

### Client Stack

client.go: `NewSession(ctx context.Context, conn net.Conn) (Session, error)`
  - negotiates protocol, returns client object
  - client object has Walk/Stat/Open/Read methods that appear like
    synchronous send/receive pairs.  Many requests can be sent in parallel,
    however (e.g. one goroutine each), and the transport will handle
    them in parallel, doing tag matching to return to the correct call.

transport.go: `func newTransport(ctx context.Context, ch Channel) roundTripper`
  - starts a `handle` goroutine to take messages off the wire
    and invoke waiting response actions in the client.
  - roundTripper uses an internal channel to communicate with the handle
    so that each roundTripper can be synchronous, while the handler
    actually handles may requests.

### Common lower-layers

channel.go: `NewChannel(conn net.Conn, msize int) Channel`
  - typical argument for codec is codec9p
  - called by ServeConn to serialize read/write from the net.Conn
  - Channel interface provides ReadFcall, WriteFcall, MSize, SetMSize

channel.go: `(ch *channel) WriteFcall(ctx context.Context, fcall *Fcall) error`
channel.go: `channel.go: (ch *channel) ReadFcall(ctx context.Context, fcall *Fcall) error`
  - check context for I/O cancellations
  - last check on Msize (may result in error)
  - for writing, call codec.Marshal, then sendmsg()
  - for reading, call readmsg, then codec.Unmarshal

messages.go: `newMessage(typ FcallType) (Message, error)`
  - called by encoding.go to write out structs (e.g. MessageTopen)
    for each type of message

encoding.go: `interface Codec`
  - provides Marshal, Unmarshal, and Size for converting between
    `[]byte` and 9P message structs.

## Server Locking Sequences

Lookup a Fid:

0. assert Fid != NOFID
1. lock session, lookup fid, unlock session
2. lock ref
   - This ensures that the ref was unlocked at some past point.
3. if ref.ent == nil, fid is being deleted, unlock and
   return "no fid"
4. return ref in locked state
   - Client unlocks when they are done with operation on Fid.
   - This forces no parallel operations on Fid at all.
   - Convention: clone fids if you want parallel access.

Clunk/Remove a Fid:

1. lock session, lookup fid, unlock session
2. lock ref, lock session, delete fid:ref, unlock session, unlock ref
3. close if ref.file != nil
4. clunk/remove ref.ent
   - This doesn't work because we may get multiple Walk/Open/Stat
     requests in parallel with Clunk. We need a way to stop
     those actions, then clunk.

Auth:

1. lock session, lookup afid (ensure not present),
   create locked aref, store afid:aref, unlock session
2. call Auth function to set aref.afile
3. unlock afid

Attach:

1. if fs.RequireAuth()):
  a. lock session, lookup aref, unlock session
  b. lock aref, call ok := aref.afile.Success(), unlock aref
  c. assert ok
2. ref := holdRef [lock session, lookup fid (ensure not present),
                   create locked ref, store afid:aref, unlock session]
3. ent, err := Root()
4. if err { lock session, delete fid:ref, unlock session, return }
5. set ref.ent = ent, unlock ref

Note:
Rather than using a locked fid lookup table, we could have
the dispatcher do fid lookups, and send requests on a channel
dedicated to (a goroutine) serving only that fid.  This may be
an optimization, or it may not - only lots of work and profiling
will tell.  So, this has not been tried.

## Copyright and license

Copyright © 2015 Docker, Inc.
Copyright © 2023 UT-Battelle LLC.
go-p9p is licensed under the Apache License,
Version 2.0. See [LICENSE](LICENSE) for the full license text.
