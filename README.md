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

dispatcher.go: `Dispatch(session Session) Handler`
  - runs a function call (from Session) for each type of messages.go:Message
    (does not see TFlush, but has ctx instead)

server.go: `ServeConn(ctx context.Context, cn net.Conn, handler Handler) error`
  - negotiates protocol (with a timeout of 1 second)
  - calls server loop, reading messages and sending them to the handler

server.go: `(c *conn) serve() error`
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

## Copyright and license

Copyright © 2015 Docker, Inc.
Copyright © 2023 UT-Battelle LLC.
go-p9p is licensed under the Apache License,
Version 2.0. See [LICENSE](LICENSE) for the full license text.

