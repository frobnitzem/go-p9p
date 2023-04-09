package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
    "path"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/chzyer/readline"
	"github.com/frobnitzem/go-p9p"
	"golang.org/x/net/context"
)

var addr string

func init() {
	flag.StringVar(&addr, "addr", "localhost:5640", "addr of 9p service")
}

func main() {
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()
    fmt.Println("Starting a pprof server on http://localhost:6060/debug/pprof.")
    fmt.Println("See https://pkg.go.dev/net/http/pprof for details.")

	ctx := context.Background()
	log.SetFlags(0)
	flag.Parse()

	proto := "tcp"
	if strings.HasPrefix(addr, "unix:") {
		proto = "unix"
		addr = addr[5:]
	}

	log.Println("dialing", addr)
	conn, err := net.Dial(proto, addr)
	if err != nil {
		log.Fatal(err)
	}

	csession, err := p9p.NewSession(ctx, conn)
	if err != nil {
		log.Fatalln(err)
	}
	msize, version := csession.Version()
	if err != nil {
		log.Fatalln(err)
	}
	log.Println("9p version", version, msize)

	commander := &fsCommander{
		ctx:     context.Background(),
		stdout:  os.Stdout,
		stderr:  os.Stderr,
	}
    err = (&commander.fs).init(commander.ctx, csession)
    if err != nil {
        log.Fatal(err)
    }
	// clone the pwd fid so we can clunk it
    pwd, err := commander.fs.root.Walk(commander.ctx)
    if err {
        log.Fatal(err)
    }
    commander.pwd = pwd

	completer := readline.NewPrefixCompleter(
		readline.PcItem("ls"),
		// readline.PcItem("find"),
		readline.PcItem("stat"),
		readline.PcItem("cat"),
		readline.PcItem("cd"),
		readline.PcItem("pwd"),
	)

	rl, err := readline.NewEx(&readline.Config{
		HistoryFile:  ".history",
		AutoComplete: completer,
	})
	if err != nil {
		log.Fatalln(err)
	}
	commander.readline = rl

	for {
        pwd := strings.Join(commander.pwd.path, "/")
		commander.readline.SetPrompt(fmt.Sprintf("%s 🐳 > ", pwd))

		line, err := rl.Readline()
		if err != nil {
			log.Fatalln("error: ", err)
		}

		if line == "" {
			continue
		}

		args := strings.Fields(line)

		name := args[0]
		var cmd func(ctx context.Context, args ...string) error

		switch name {
		case "ls":
			cmd = commander.cmdls
		case "cd":
			cmd = commander.cmdcd
		case "pwd":
			cmd = commander.cmdpwd
		case "cat":
			cmd = commander.cmdcat
		default:
			cmd = func(ctx context.Context, args ...string) error {
				return fmt.Errorf("command not implemented")
			}
		}

		ctx, _ = context.WithTimeout(commander.ctx, 5*time.Second)
		if err := cmd(ctx, args[1:]...); err != nil {
			if err == p9p.ErrClosed {
				log.Println("connection closed, shutting down")
                csession.Stop(err)
				return
			}

			log.Printf("👹 %s: %v", name, err)
		}
	}
}

// State of a filesystem as seen from the client side.
type fsState struct {
    session p9p.Session
    //fids    map[*dirEnt]p9p.Fid
	nextfid p9p.Fid // starts at rootfid, since newFid increments, then returns
	root    dirEnt  // what holds the rootfid
}

// Initializes a session by sending an Attach,
// and storing all the relevant session data.
// Does no cleanup (assumes no old state).
func (fs *fsState) init(ctx context.Context, session p9p.Session) error {
    rootFid = p9p.Fid(1)
	_, err := session.Attach(ctx, rootfid, p9p.NOFID, "anyone", "/")
    if err != nil {
        return err
	}

    fs.session = session
    fs.fids = make(map[*dirEnt]p9p.Fid)
    fs.nextfid = rootFid
    fs.root = dirEnt{
        path: make([]string,0),
        depth: 0,
        fid: rootFid,
        fs: fs,
    }
}

func (fs *fsState) newFid() p9p.Fid {
    fs.nextfid++
    return fs.nextfid
}

type Warning struct {
    string
}
func (w Warning) Error() {
    return w
}

type dirEnt struct {
    path  []string // absolute path
	fid   p9p.Fid
    fs    *fsState
}

var noEnt dirEnt = dirEnt{nil, p9p.NOFID, nil}

// New entry has no path set yet!
func (fs *fsState) newEnt() dirEnt {
    return dirEnt{
        fid: dir.fs.newFid(),
        fs:  fs,
    }
}

type fsCommander struct {
	ctx     context.Context
	pwd     dirEnt
    fs      fsState

	readline *readline.Instance
	stdout   io.Writer
	stderr   io.Writer
}

// Determine the starting dirEnt and path elements to send
// to Walk() in order to reach path p.
// pwd is absolute and rel is a (potentially) relative location
func (ent dirEnt) toWalk(p string) (dirEnt, []string, error) {
    abs := path.IsAbs(p)
    steps, bsp := p9p.NormalizePath(strings.Split(strings.Trim(p, "/"), "/"))

    if abs {
        if bsp != 0 {
            return d.fs.root, nil, errors.New("invalid path: "+p)
        }
        return d.fs.root, steps, nil
    }

    if bsp < 0 {
        return ent, nil, errors.New("invalid path: "+p)
    }

    return ent, steps, nil
}

func (ent dirEnt) Clunk(ctx context.Context) error {
    return ent.fs.session.Clunk(ctx, ent.fid)
}

// Note: This always returns returns a file with a nonzero IOUnit.
func (ent dirEnt) Open(ctx context.Context, mode Flag) (p9p.File, error) {
    _, iounit, err := ent.fs.session.Open(ctx, ent.fid, p9p.OREAD)
	if iounit < 1 {
		msize, _ := ent.fs.session.Version()
        // size of message max minus fcall io header (Rread)
		iounit = uint32(msize - 11)
	}
    return fileRef{ent, iounit}, err
}
type fileRef struct {
    Ent
    iounit int
}
func (f fileRef) Read(ctx context.Context, p []byte, offset int64) (int, error) {
    return f.fs.session.Read(ctx, f.fid, p, offset)
}
func (f fileRef) Write(ctx context.Context, p []byte, offset int64) (int, error) {
    return f.fs.session.Write(ctx, f.fid, p, offset)
}
func (f fileRef) IOUnit() int {
    return f.iounit
}

type openDir struct {
    fileRef
    done bool
    nread int
    p []byte
}

// Note: This always returns returns a file with a nonzero IOUnit,
// (because dirEnt.Open does)
func (ent dirEnt) OpenDir(ctx context.Context) (NameReader, error) {
    file, err := ent.Open(ctx, p9p.OREAD)
	if err != nil {
		return openDir{}, err
	}
    return openDir{
        file,
        done: false,
        nread: 0,
        p: make([]byte, iounit),
    }
}

func (dir openDir) Next(ctx context.Context) ([]p9p.Dir, error) {
    if dir.done {
        return nil, nil
    }
    var n int
    var err error

    n, err = dir.Read(ctx, p, dir.nread)
    if err != nil {
        return nil, err
    }

    rd := bytes.NewReader(file.p[:n])
    codec := p9p.NewCodec() // TODO(stevvooe): Need way to resolve codec based on session.
    ret := make([]p9p.Dir, 0, 10)
    for {
        var d p9p.Dir
        if err = p9p.DecodeDir(codec, rd, &d); err != nil {
            if err == io.EOF {
                err = nil
            }
            break
        }
        ret = append(ret, d)
    }
    if len(ret) == 0 {
        dir.done = true
    }
    dir.nread += len(ret)
    return ret, err
}

func (ent dirEnt) Stat(ctx context.Context) (Dir, error) {
    return ent.fs.session.Stat(ctx, ent.fid)
}
func (ent dirEnt) WStat(ctx context.Context, stat Dir) error {
    return ent.fs.session.WStat(ctx, ent.fid, stat)
}
func (ent dirEnt) Walk(ctx context.Context,
                       names ...string) ([]p9p.Qid, Dirent, error) {
    steps, bsp := p9p.NormalizePath(names)
    if bsp < 0 || bsp > len(ent.path) {
        return nil, ent, errors.New("invalid path: "+p)
    }

    next = ent.fs.newEnt()
    qids, err := c.session.Walk(c.ctx, ent.fid, next.fid, steps...)
    if err != nil {
		return nil, ent, err
	}
    if len(qids) != len(names) { // incomplete = failure to get new ent
        return qids, noEnt, Warning("Incomplete walk result")
    }
    // drop part of ent.path
    steps = steps[:len(qids)]
    remain := len(ent.path) - bsp
    next.path = append(ent.path[:remain], steps[bsp:]...)

    return qids, next, nil
}

func (c *fsCommander) cmdls(ctx context.Context, args ...string) error {
	ps := []string{""}
	if len(args) > 0 {
		ps = args
	}

	wr := tabwriter.NewWriter(c.stdout, 0, 8, 8, ' ', 0)

	for _, p := range ps {
		// create a header if have more than one path.
		if len(ps) > 1 {
			fmt.Fprintln(wr, p+":")
		}

        rel, steps, err := c.pwd.toWalk(p)
        if err != nil {
            return err
        }

		qid, ent, err := rel.Walk(ctx, steps...)
        if err != nil || len(qid) != len(steps) {
			return err
		}
		defer ent.Clunk(ctx)
        
        if qid.Type&QTDIR == 0 { // non-dir.
            d, err := ent.Stat(ctx)
            if err != nil {
                return err
            }
            fmt.Fprintf(wr, "%v\t%v\t%v\t%s\n", os.FileMode(d.Mode), d.Length, d.ModTime, d.Name)
        } else {
            file, err := ent.OpenDir(ctx)
            if err != nil {
                return err
            }
            defer file.Close(ctx)

            for {
                dirs, err := reader.Next(ctx)
                if len(dirs) == 0 {
                    break
                }
                for _, d := range(dirs) {
                    fmt.Fprintf(wr, "%v\t%v\t%v\t%s\n", os.FileMode(d.Mode), d.Length, d.ModTime, d.Name)
                }
            }
        }

		if len(ps) > 1 {
			fmt.Fprintln(wr, "")
		}
	}

	// all output is dumped only after success.
	return wr.Flush()
}

func (c *fsCommander) cmdcd(ctx context.Context, args ...string) error {
	var p string
	switch len(args) {
	case 0:
		p = "/"
	case 1:
		p = args[0]
	default:
		return fmt.Errorf("cd: invalid args: %v", args)
	}

    rel, steps, err := c.pwd.toWalk(p)
    if err {
        return err
    }

    qids, next, err := rel.Walk(ctx, steps...)
    if err != nil || len(qid) != len(steps) {
        return err
    }

	log.Println("cd", p, c.pwd, " ~> ", next)
	c.pwd.Clunk(ctx)
	c.pwd = next

	return nil
}

func (c *fsCommander) cmdpwd(ctx context.Context, args ...string) error {
	if len(args) != 0 {
		return fmt.Errorf("pwd takes no arguments")
	}

	fmt.Println( strings.Join(c.pwd.path, "/") )
	return nil
}

func (c *fsCommander) cmdcat(ctx context.Context, args ...string) error {
	var p string
	switch len(args) {
	case 0:
		p = "/"
	case 1:
		p = args[0]
	default:
		return fmt.Errorf("cd: invalid args: %v", args)
	}

    rel, steps, err := c.pwd.toWalk(p)
    if err {
        return err
    }

    qids, ent, err := rel.Walk(ctx, steps...)
    if err != nil || len(qids) != len(steps) {
        return err
    }
	defer ent.Clunk(ctx)

	file, err := ent.Open(ctx, p9p.OREAD)
	if err != nil {
		return err
	}
    defer file.Close(ctx)

	b := make([]byte, file.IOUnit())

	n, err := file.Read(ctx, b, 0)
	if err != nil {
		return err
	}

	if _, err := os.Stdout.Write(b[:n]); err != nil {
		return err
	}

	os.Stdout.Write([]byte("\n"))

	return nil
}
