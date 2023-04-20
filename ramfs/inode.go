package ramfs

import (
	"errors"
	"sync"
	"github.com/frobnitzem/go-p9p"
)

// Node in a rooted digraph.  Following the first parent
// always leads to root (whose first parent is itself).
// Use IsDir() to determine whether it's a directory or not.
// Only leaf nodes (non-directories) have Data
// Only directories have len(children) > 0.
type FileEnt struct {
	sync.Mutex
	parents  []*FileEnt
	children []*FileEnt

	fs   *fServer
	Info p9p.Dir
	Data []byte
}

func (f *FileEnt) link_child(c *FileEnt) {
	f.Lock()
	defer f.Unlock()
	// FIXME: need a non-blocking retry-loop here
	c.Lock()
	defer c.Unlock()

	f.children = append(f.children, c)
	c.parents = append(c.parents, f)
}

func (f *FileEnt) Remove() error {
	f.Lock()
	defer f.Unlock()
	for _, c := range f.children {
		if c.parents[0] == f {
			return errors.New("Cannot delete non-empty dir.")
		}
	}
	for i, c := range f.children {
		// FIXME: need a non-blocking retry-loop here
		c.Lock()
		// Should not occur, but now we have an incomplete, weakly linked dir.
		if c.parents[0] == f {
			c.Unlock()
			f.children = f.children[i:]
			return errors.New("Cannot delete non-empty dir.")
		}
		c.parents = dropEnt(f, c.parents)
		c.Unlock()
	}
	for _, p := range f.parents {
		p.Lock()
		// Technically, each child should be unique,
		// dropEnt should always drop exactly 1 element here.
		p.children = dropEnt(f, p.children)
		p.Unlock()
	}
	return nil
}

func dropEnt(x *FileEnt, lst []*FileEnt) []*FileEnt {
	i := 0
	for j := 0; j < len(lst); j++ {
		lst[i] = lst[j]
		if lst[i] != x { // keep
			i++
		}
	}
	return lst[:i]
}
