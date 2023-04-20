package ramfs

import (
	"errors"
	"testing"

	p9p "github.com/frobnitzem/go-p9p"
)

// check that p links exactly once to f
func (p *FileEnt) hasOneChild(f *FileEnt) bool {
	n := 0
	for _, c := range p.children {
		if c == f {
			n++
		}
	}
	return n == 1
}

// check that c links exactly once to f
func (c *FileEnt) hasOneParent(f *FileEnt) bool {
	n := 0
	for _, p := range c.parents {
		if p == f {
			n++
		}
	}
	return n == 1
}

func (fs *fServer) validate() error {
	// Count of parents, populated as elem. is added to todo.
	tgts := make(map[*FileEnt]int)

	a := fs.root
	if len(a.parents) < 1 || a.parents[0] != a {
		return errors.New("root has invalid parents.")
	}

	tgts[a] = 1
	todo := []*FileEnt{a}

	for len(todo) > 0 {
		f := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if !f.IsDir() {
			if len(f.children) > 0 {
				return errors.New(f.Info.Name + ": file cannot contain child refs")
			}
		}
		for _, c := range f.children {
			if !f.hasOneChild(c) {
				return errors.New(f.Info.Name + " incorrect child refs to " + c.Info.Name)
			}
			if !c.hasOneParent(f) {
				return errors.New(c.Info.Name + " has incorrect parent refs to " + f.Info.Name)
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
		if n != len(f.parents) {
			return errors.New("Node " + f.Info.Name + " has extra parent references.")
		}
	}

	// TODO: check that following first parent is non-cyclic
	return nil
}

func TestFS(t *testing.T) {
	fs := newServer()
	if err := fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}
	a := fs.root

	x1 := fs.Create(a, fs.dir("x1", true))
	if err := fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}

	x2 := fs.Create(a, fs.dir("x2", true))
	if err := fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}

	x3 := fs.Create(a, fs.dir("x3", false))
	if err := fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}

	y1 := fs.Create(x1, fs.dir("y1", false))
	if err := fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}

	y2 := fs.Create(x1, fs.dir("y2", true))
	if err := fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}

	if err := y2.Remove(); err != nil {
		t.Fatalf("unable to remove:  %v", err)
	}
	if err := fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}

	// create a child of a file (producing an invalid FS)
	z1 := fs.Create(y1, fs.dir("z1", false))
	if err := fs.validate(); err == nil {
		t.Fatalf("did not detect invalid file-as-parent.")
	}
	if err := z1.Remove(); err != nil {
		t.Fatalf("unable to remove:  %v", err)
	}
	if err := fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}

	// done with tests
	n := len([]*FileEnt{x1, x2, x3, y1, y2})
	if n == 0 {
		t.Fatalf("meh")
	}
}

func (fs *fServer) dir(fname string, isDir bool) p9p.Dir {
	return fs.newDir(fname, isDir, "root", 0755)
}

// Create a filesystem with a simple cycle
func TestCyc(t *testing.T) {
	fs := newServer()
	if err := fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}
	a := fs.root
	b := fs.Create(a, fs.dir("b", true))
	c := fs.Create(b, fs.dir("c", true))
	c.link_child(a)
	if len(a.parents) != 2 || a.parents[1] != c {
		t.Fatalf("cyclic link unsuccessful.")
	}

	if err := fs.validate(); err != nil {
		t.Fatalf("invalid fs:  %v", err)
	}

	if err := b.Remove(); err == nil {
		t.Fatalf("should not be able to remove.")
	}
	if err := c.Remove(); err != nil {
		t.Fatalf("should be able to remove.")
	}
}
