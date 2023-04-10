package main

import (
	"bytes"
	"errors"
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
		ctx:    context.Background(),
		stdout: os.Stdout,
		stderr: os.Stderr,
	}
	err = (&commander.fs).init(commander.ctx, csession)
	if err != nil {
		log.Fatal(err)
	}
	// clone the pwd fid so we can clunk it
	_, pwd, err := commander.fs.root.Walk(commander.ctx)
	if err != nil {
		log.Fatal(err)
	}
	pwd1, ok := pwd.(dirEnt)
	if !ok {
		log.Fatal(errors.New("bad pwd"))
	}
	commander.pwd = pwd1

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
	//fids	map[*dirEnt]p9p.Fid
	nextfid p9p.Fid // starts at rootfid, since newFid increments, then returns
	root    dirEnt  // what holds the rootfid
}

// Initializes a session by sending an Attach,
// and storing all the relevant session data.
// Does no cleanup (assumes no old state).
func (fs *fsState) init(ctx context.Context, session p9p.Session) error {
	rootFid := p9p.Fid(1)
	qid, err := session.Attach(ctx, rootFid, p9p.NOFID, "anyone", "/")
	if err != nil {
		return err
	}

	fs.session = session
	//fs.fids = make(map[*dirEnt]p9p.Fid)
	fs.nextfid = rootFid
	fs.root = dirEnt{
		path: make([]string, 0),
		fid:  rootFid,
		qid:  qid,
		fs:   fs,
	}
	return nil
}

func (fs *fsState) newFid() p9p.Fid {
	fs.nextfid++
	return fs.nextfid
}

type Warning struct {
	s string
}

func (w Warning) Error() string {
	return w.s
}

type dirEnt struct {
	path []string // absolute path
	fid  p9p.Fid
	qid  p9p.Qid // FIXME(frobnitzem): stash qids here
	fs   *fsState
}

var noEnt dirEnt = dirEnt{nil, p9p.NOFID, p9p.Qid{}, nil}

type fileRef struct {
	dirEnt
	iounit int
}

var noFile fileRef = fileRef{noEnt, 0}

// New entry has no path set yet!
func (fs *fsState) newEnt() dirEnt {
	return dirEnt{
		fid: fs.newFid(),
		fs:  fs,
	}
}

type fsCommander struct {
	ctx context.Context
	pwd dirEnt
	fs  fsState

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
			return ent.fs.root, nil, errors.New("invalid path: " + p)
		}
		return ent.fs.root, steps, nil
	}

	if bsp < 0 {
		return ent, nil, errors.New("invalid path: " + p)
	}

	return ent, steps, nil
}

// Note: This always returns returns a file with a nonzero IOUnit.
func (ent dirEnt) Open(ctx context.Context, mode p9p.Flag) (p9p.File, error) {
	_, iounit, err := ent.fs.session.Open(ctx, ent.fid, p9p.OREAD)
	iou := int(iounit)
	if iounit < 1 {
		msize, _ := ent.fs.session.Version()
		// size of message max minus fcall io header (Rread)
		iou = msize - 11
	}
	return fileRef{ent, iou}, err
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
func (f fileRef) Close(ctx context.Context) error {
	return nil
}

type openDir struct {
	fileRef
	done  bool
	nread int64
	buf   []byte
}

// Note: This always returns returns a file with a nonzero IOUnit,
// (because dirEnt.Open does)
func (ent dirEnt) OpenDir(ctx context.Context) (p9p.ReadNext, error) {
	file, err := ent.Open(ctx, p9p.OREAD)
	if err != nil {
		return nil, err
	}
	ref, ok := file.(fileRef)
	if !ok {
		return nil, errors.New("Invalid return value from Open")
	}
	dir := openDir{
		ref,
		false,
		0,
		make([]byte, file.IOUnit()),
	}
	return dir.Next, nil
}

func (dir *openDir) Next(ctx context.Context) ([]p9p.Dir, error) {
	if dir.done {
		return nil, nil
	}
	var n int
	var err error

	n, err = dir.Read(ctx, dir.buf, dir.nread)
	if err != nil {
		if err == io.EOF {
			dir.done = true
			return nil, nil
		}
		return nil, err
	}
	dir.nread += int64(n)

	rd := bytes.NewReader(dir.buf[:n])
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
	return ret, err
}

func (ent dirEnt) Qid() p9p.Qid {
	return ent.qid
}

func (ent dirEnt) Create(ctx context.Context, name string,
	perm uint32, mode p9p.Flag) (p9p.Dirent, p9p.File, error) {
	if name == "." || name == ".." || strings.Contains(name, "/\\") {
		return noEnt, noFile, errors.New("Invalid filename")
	}
	if !p9p.IsDir(ent) {
		return noEnt, noFile, p9p.ErrCreatenondir
	}
	qid, iounit, err := ent.fs.session.Create(ctx, ent.fid, name, perm, mode)
	if err != nil {
		return noEnt, noFile, err
	}
	ent.path = append(ent.path, name)
	ent.qid = qid

	// TODO(frobnitzem): this appears twice, make a fileEnt function.
	iou := int(iounit)
	if iounit < 1 {
		msize, _ := ent.fs.session.Version()
		// size of message max minus fcall io header (Rread)
		iou = msize - 11
	}
	return ent, fileRef{ent, iou}, err
}
func (ent dirEnt) Stat(ctx context.Context) (p9p.Dir, error) {
	return ent.fs.session.Stat(ctx, ent.fid)
}
func (ent dirEnt) WStat(ctx context.Context, stat p9p.Dir) error {
	return ent.fs.session.WStat(ctx, ent.fid, stat)
}
func (ent dirEnt) Clunk(ctx context.Context) error {
	return ent.fs.session.Clunk(ctx, ent.fid)
}
func (ent dirEnt) Remove(ctx context.Context) error {
	return ent.fs.session.Remove(ctx, ent.fid)
}
func (ent dirEnt) Walk(ctx context.Context,
	names ...string) ([]p9p.Qid, p9p.Dirent, error) {
	steps, bsp := p9p.NormalizePath(names)
	if bsp < 0 || bsp > len(ent.path) {
		return nil, ent, errors.New("invalid path: " + path.Join(names...))
	}

	next := ent.fs.newEnt()
	qids, err := ent.fs.session.Walk(ctx, ent.fid, next.fid, steps...)
	if err != nil {
		return nil, noEnt, err
	}
	if len(qids) != len(names) { // incomplete = failure to get new ent
		return qids, noEnt, Warning{"Incomplete walk result"}
	}
	// drop part of ent.path
	steps = steps[:len(qids)]
	remain := len(ent.path) - bsp
	next.path = append(ent.path[:remain], steps[bsp:]...)
	if len(qids) > 0 {
		next.qid = qids[len(qids)-1]
	} else {
		next.qid = ent.qid
	}

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

		qids, ent, err := rel.Walk(ctx, steps...)
		if err != nil || len(qids) != len(steps) {
			return err
		}
		defer ent.Clunk(ctx)

		//qid := ent.Qid()

		if !p9p.IsDir(ent) { // non-dir.
			d, err := ent.Stat(ctx)
			if err != nil {
				return err
			}
			fmt.Fprintf(wr, "%v\t%v\t%v\t%s\n", os.FileMode(d.Mode), d.Length, d.ModTime, d.Name)
		} else {
			reader, err := ent.OpenDir(ctx)
			if err != nil {
				return err
			}
			//defer file.Close(ctx)

			for {
				dirs, err := reader(ctx)
				if err != nil {
					return err
				}
				if len(dirs) == 0 {
					break
				}
				for _, d := range dirs {
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
	if err != nil {
		return err
	}

	qids, next, err := rel.Walk(ctx, steps...)
	if err != nil || len(qids) != len(steps) {
		return err
	}
	if !p9p.IsDir(next) {
		next.Clunk(ctx)
		return errors.New("cd: not a directory.")
	}

	pwd1, ok := next.(dirEnt)
	if !ok {
		next.Clunk(ctx)
		return errors.New("non-dir returned from walk")
	}
	c.pwd.Clunk(ctx)
	c.pwd = pwd1

	return nil
}

func (c *fsCommander) cmdpwd(ctx context.Context, args ...string) error {
	if len(args) != 0 {
		return fmt.Errorf("pwd takes no arguments")
	}

	fmt.Println(strings.Join(c.pwd.path, "/"))
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
	if err != nil {
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
	//defer file.Close(ctx)

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
