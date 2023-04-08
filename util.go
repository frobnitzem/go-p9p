package p9p

import "strings"

// Returns false if any path elements contain '/' or '\'
// or if the number of ".."-s is more than depth
func ValidPath(args []string, depth int) bool {
	for _, s := range args {
		if s == ".." {
			depth--
			if depth < 0 {
				return false
			}
		}

		if strings.ContainsAny(s, "\\/") {
			return false
		}
	}
	return true
}

// Normalize a path by removing all ”, '.', and treating
// all '..' as backspaces.  The result may only
// contain '..' elements at the beginning of the path.
// Functional, so it effectively copies the path.
//
// Note: path.Clean does this probably more efficiently,
// but doesn't leave .. at the root, which we sometimes need.
func NormalizePath(args []string) ([]string, bool) {
	ans := make([]string, len(args))

	cursor := 0
	lo := 0 // highest non-.. entry
	for _, s := range args {
		if strings.ContainsAny(s, "\\/") {
            return nil, false
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
	return ans[:cursor], true
}
