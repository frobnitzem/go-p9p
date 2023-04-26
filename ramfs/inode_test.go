package ramfs

import (
	"errors"
	"testing"
	"context"

	p9p "github.com/frobnitzem/go-p9p"
)

// check that p links exactly once to f
func (p *FileEnt) hasOneChild(f *FileEnt) bool {
	_, ok := p.children[f.Info.Name]
	return ok
}

func (fs *fServer) validate() error {
	// Count of parents, populated as elem. is added to todo.
	tgts := make(map[*FileEnt]int)

	tgts[fs.root] = 1
	todo := []*FileEnt{fs.root}

	for len(todo) > 0 {
		f := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if !f.IsDir() {
			if f.children != nil {
				return errors.New(f.Info.Name + ": file cannot contain child refs")
			}
		}
		for _, c := range f.children {
			if !f.hasOneChild(c) {
				return errors.New(f.Info.Name + " incorrect child refs to " + c.Info.Name)
			}
			n, ok := tgts[c]
			if !ok { // enqueue to visit
				tgts[c] = 1
				todo = append(todo, c)
			} else {
				tgts[c] = n + 1
			}
		}
	}
	// check that there are no extra parent links
	// from each of the visited nodes.
	for f, n := range tgts {
		if n != f.nref {
			return errors.New("Node " + f.Info.Name + " has extra references.")
		}
	}

	// TODO: check that following first parent is non-cyclic
	return nil
}

func TestFS(t *testing.T) {
	ctx := context.Background()
	fs1 := NewServer(ctx)
	fs, ok := fs1.(*fServer)
	if !ok {
		t.Fatalf("not an fServer fs:  %v", fs)
	}
	if err := fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}
	a := fs.root

	x1, err := fs.Create(a, fs.dir("x1", true))
	if err != nil {
		t.Fatalf("create err:  %v", err)
	}
	if err = fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}

	x2, err := fs.Create(a, fs.dir("x2", true))
	if err != nil {
		t.Fatalf("create err:  %v", err)
	}
	if err = fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}

	x3, err := fs.Create(a, fs.dir("x3", false))
	if err != nil {
		t.Fatalf("create err:  %v", err)
	}
	if err = fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}

	y1, err := fs.Create(x1, fs.dir("y1", false))
	if err != nil {
		t.Fatalf("create err:  %v", err)
	}
	if err = fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}

	y2, err := fs.Create(x1, fs.dir("y2", true))
	if err != nil {
		t.Fatalf("create err:  %v", err)
	}
	if err = fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}

	//if err = y2.Remove(); err != nil {
	//	t.Fatalf("unable to remove:  %v", err)
	//}
	//if err = fs.validate(); err != nil {
	//	t.Fatalf("invalid fs:  %v", err)
	//}

	// create a child of a file (producing an invalid FS)
	z1, err := fs.Create(y1, fs.dir("z1", false))
	if err == nil {
		t.Fatalf("did not detect invalid file-as-parent.")
	}
	//if err = z1.Remove(); err != nil {
	//	t.Fatalf("unable to remove:  %v", err)
	//}
	//if err = fs.validate(); err != nil {
	//	t.Fatalf("invalid fs:  %v", err)
	//}

	// done with tests
	n := len([]*FileEnt{x1, x2, x3, y1, y2, z1})
	if n == 0 {
		t.Fatalf("meh")
	}
}

func (fs *fServer) dir(fname string, isDir bool) p9p.Dir {
	mode := uint32(0755)
	if isDir {
		mode |= p9p.DMDIR
	}
	return newDir(fs.next(), fname, "root", mode)
}

// Create a filesystem with a simple cycle
func TestCyc(t *testing.T) {
	ctx := context.Background()
	fs1 := NewServer(ctx)
	fs, ok := fs1.(*fServer)
	if !ok {
		t.Fatalf("not an fServer fs:  %v", fs)
	}
	if err := fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}
	a := fs.root
	b, err := fs.Create(a, fs.dir("b", true))
	if err != nil {
		t.Fatalf("create err:  %v", err)
	}
	c, err := fs.Create(b, fs.dir("c", true))
	if err != nil {
		t.Fatalf("create err:  %v", err)
	}
	a.nref += 1
	err = c.link_child(a.Info.Name, a)
	if err != nil {
		t.Fatalf("cyclic link unsuccessful: %v", err)
	}

	if err = fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}

	//if err = b.Remove(); err == nil {
	//	t.Fatalf("should not be able to remove.")
	//}
	//if err = c.Remove(); err != nil {
	//	t.Fatalf("should be able to remove.")
	//}
	//if err = fs.validate(); err != nil {
	//	t.Fatalf("invalid fs:  %v", err)
	//}
}
