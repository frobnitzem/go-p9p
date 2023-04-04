# p9p

[![GoDoc](https://godoc.org/github.com/frobnitzem/go-p9p?status.svg)](https://godoc.org/github.com/frobnitzem/go-p9p) [![Apache licensed](https://img.shields.io/badge/license-Apache-blue.svg)](https://raw.githubusercontent.com/frobnitzem/go-p9p/master/LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/frobnitzem/go-p9p)](https://goreportcard.com/report/github.com/frobnitzem/go-p9p) [![Badge Badge](https://aleen42.github.io/badges/src/ferrari.svg)](https://www.gokgs.com/)
---

A modern, performant 9P library for Go.

For information on usage, please see the [GoDoc](https://godoc.org/github.com/frobnitzem/go-p9p).

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


## Copyright and license

Copyright © 2015 Docker, Inc.
Copyright © 2023 UT-Battelle LLC.
go-p9p is licensed under the Apache License,
Version 2.0. See [LICENSE](LICENSE) for the full license text.

