package p9p

import (
	"path"
	"strings"
)

// Find the absolute path of names relative to dir.
//
// dir must be a valid internal path.
// names are validated.  They are not re-ordered
// or changed (e.g. to process "a/../" etc.), so
// names that contain ".", "", or non-".." before ".."
// will return an error.
//
// On success, the result is always a valid internal path.
func WalkName(dir string, names ...string) (string, error) {
	depth := strings.Count(dir[:len(dir)-1], "/")
	bsp := ValidPath(names)
	if bsp < 0 || bsp > depth {
		//fmt.Println("Invalid path: ", strings.Join(names, "/"))
		//fmt.Println("dir: ", dir, "depth: ", depth, " bsp: ", bsp)
		return dir, MessageRerror{Ename: "Invalid path"}
	}

	return path.Join(dir, path.Join(names...)), nil
}

// Find the result of Create(name) from the directory dir.
// Checks that name is valid (i.e. does not contain slashes,
// and is not "", "." or "..").
//
// On success, the result is always a valid internal path.
func CreateName(dir string, name string) (string, error) {
	if strings.ContainsAny(name, "\\/") || len(name) == 0 || name == "." || name == ".." {
		return "", MessageRerror{Ename: "Invalid path"}
	}
	return path.Join(dir, name), nil
}

// Returns -1 if any path elements are '.' or
// contain characters '/' or '\'
// or if .. follows a non-..
// Otherwise, returns the number of leading .. elements.
func ValidPath(args []string) int {
	n := 0
	for i, s := range args {
		if len(s) == 0 || s == "." {
			return -1
		} else if s == ".." {
			if n != i {
				return -1
			}
			n++
		} else {
			if strings.ContainsAny(s, "\\/") {
				return -1
			}
		}
	}
	return n
}

// Normalize a path by removing all "" and "." elements,
// and treating all ".." as backspaces.  The result may only
// contain ".." elements at the beginning of the path.
// Functional, so it effectively copies the path.
//
// Returns (cleaned path, backspaces)
// where backspaces = -1 in case of an error
// or else indicates the number of leading ".." elements
// in case of success.
//
// Note: path.Clean does this probably more efficiently,
// but doesn't leave .. at the root, which we need.
func NormalizePath(args []string) ([]string, int) {
	ans := make([]string, len(args))

	cursor := 0
	lo := 0 // highest non-.. entry
	for _, s := range args {
		if strings.ContainsAny(s, "\\/") {
			return nil, -1
		}
		if len(s) == 0 || s == "." { // skip
			continue
		}
		if s == ".." { // pop
			if cursor > lo {
				cursor--
				continue
			}
			// can't pop, fall-through
			lo++
		}
		ans[cursor] = s
		cursor++
	}
	return ans[:cursor], lo
}

// Determine the starting Dirent and path elements to send
// to Walk() in order to reach path p.
func ToWalk(ent Dirent, p string) (isAbs bool, steps []string, err error) {
	var bsp int
	isAbs = path.IsAbs(p)
	steps, bsp = NormalizePath(strings.Split(strings.Trim(p, "/"), "/"))

	if isAbs {
		if bsp != 0 {
			return true, nil, MessageRerror{"invalid path: " + p}
		}
		return true, steps, nil
	}

	if bsp < 0 {
		return false, nil, MessageRerror{"invalid path: " + p}
	}

	return false, steps, nil
}
