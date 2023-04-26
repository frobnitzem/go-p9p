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
	nref int
	children map[string]*FileEnt

	fs   *fServer
	Info p9p.Dir
	Data []byte
}

func (f *FileEnt) incref() string {
	f.Lock()
	defer f.Unlock()

	f.nref++
	return f.Info.Name
}

// Note: For this to work, incref must never
// be called after decref() has left the ent
// at state nref = 0
func (f *FileEnt) decref() int {
	f.Lock()
	f.nref--
	n := f.nref
	f.Unlock()

	if n == 0 && f.children != nil { // trigger child deletion
		for _, c := range(f.children) {
			c.decref()
		}
		f.children = nil
	}
	return n
}

// c.incref() should already have been called, so there
// is no chance that c will be deleted during this call.
// If this call returns an error, c.decref should be called.
func (f *FileEnt) link_child(name string, c *FileEnt) error {
	f.Lock()
	defer f.Unlock()

	if f.children == nil {
		return errors.New("not a directory.")
	}
	_, found := f.children[name]
	if found {
		return errors.New("duplicate file name")
	}
	f.children[name] = c
	return nil
}

// Opposite of link_child
// Caller is responsible for calling c.decref *after* this
// routine returns successfully (error == nil).
func (f *FileEnt) unlink_child(name string) error {
	if f.children == nil {
		return errors.New("not a directory.")
	}

	f.Lock()
	defer f.Unlock()
	_, found := f.children[name]
	if !found {
		return errors.New("not found")
	}
	delete(f.children, name)

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
