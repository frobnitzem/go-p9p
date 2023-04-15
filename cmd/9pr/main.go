package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/chzyer/readline"
	"github.com/frobnitzem/go-p9p"
	"golang.org/x/net/context"
)

var (
	addr string
	perf bool
)

func init() {
	flag.StringVar(&addr, "addr", "localhost:5640", "addr of 9p service")
	flag.BoolVar(&perf, "perf", false, "Run a performance profile server?")
}

func main() {
	if perf {
		fmt.Println("Starting a pprof server on http://localhost:6060/debug/pprof")
		fmt.Println("See https://pkg.go.dev/net/http/pprof for details.")
		go func() {
			log.Println(http.ListenAndServe("localhost:6060", nil))
		}()
	}

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

	csession, err := p9p.CSession(ctx, conn)
	if err != nil {
		log.Fatalln(err)
	}
	msize, version := csession.Version()
	if err != nil {
		log.Fatalln(err)
	}
	log.Println("9p version", version, msize)

	fs := p9p.CFileSys(csession)
	root, err := fs.Attach(ctx, "anonymous", "/", nil)
	if err != nil {
		log.Fatal(err)
	}
	// clone the pwd fid so we can clunk it
	_, pwd, err := root.Walk(ctx)
	if err != nil {
		log.Fatal(err)
	}
	commander := &fsCommander{
		ctx:    ctx,
		stdout: os.Stdout,
		stderr: os.Stderr,
		root:   root,
		pwd:    pwd,
	}

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
		pwd := commander.path
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
		case "write":
			cmd = commander.cmdwrite
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

type fsCommander struct {
	ctx  context.Context
	pwd  p9p.Dirent
	root p9p.Dirent
	path string

	readline *readline.Instance
	stdout   io.Writer
	stderr   io.Writer
}

func (c *fsCommander) toWalk(p string) (p9p.Dirent, []string, error) {
	isAbs, steps, err := p9p.ToWalk(c.pwd, p)
	rel := c.pwd
	if isAbs {
		rel = c.root
	}
	return rel, steps, err
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

		rel, steps, err := c.toWalk(p)
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
		return fmt.Errorf("invalid args: %v", args)
	}

	rel, steps, err := c.toWalk(p)
	if err != nil {
		return err
	}

	qids, next, err := rel.Walk(ctx, steps...)
	if err != nil || len(qids) != len(steps) {
		return err
	}
	if !p9p.IsDir(next) {
		next.Clunk(ctx)
		return errors.New("not a directory.")
	}

	c.pwd.Clunk(ctx)
	c.pwd = next

	return nil
}

func (c *fsCommander) cmdpwd(ctx context.Context, args ...string) error {
	if len(args) != 0 {
		return fmt.Errorf("pwd takes no arguments")
	}

	fmt.Println(c.path)
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
		return fmt.Errorf("invalid args: %v", args)
	}

	rel, steps, err := c.toWalk(p)
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

func (c *fsCommander) cmdwrite(ctx context.Context, args ...string) error {
	p := args[0]

	rel, steps, err := c.toWalk(p)
	if err != nil {
		return err
	}

	qids, ent, err := rel.Walk(ctx, steps...)
	if err != nil || len(qids) != len(steps) {
		return err
	}
	defer ent.Clunk(ctx)

	file, err := ent.Open(ctx, p9p.OWRITE)
	if err != nil {
		return err
	}
	//defer file.Close(ctx)

	b := []byte(strings.Join(args[1:], " "))

	// WARNING: refuses to do a 0-byte write.
	for nwritten := int64(0); len(b) > 0; {
		n, err := file.Write(ctx, b, nwritten)
		if err != nil {
			return err
		}
		b = b[n:]
		nwritten += int64(n)
	}

	return nil
}
