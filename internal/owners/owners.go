// Package owners reads a repository's CODEOWNERS file to work out which of a
// pull request's files fall to one reviewer.
package owners

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

type rule struct {
	pattern string
	owners  []string
}

// File is a parsed CODEOWNERS.
type File struct{ rules []rule }

// Load finds CODEOWNERS in a checkout (.github/, the root, then docs/). It
// returns nil when the repository has none.
func Load(checkout string) *File {
	for _, p := range []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"} {
		b, err := os.ReadFile(filepath.Join(checkout, p))
		if err == nil {
			return Parse(string(b))
		}
	}
	return nil
}

func Parse(text string) *File {
	f := &File{}
	for _, ln := range strings.Split(text, "\n") {
		if i := strings.Index(ln, "#"); i >= 0 {
			ln = ln[:i]
		}
		fields := strings.Fields(ln)
		if len(fields) < 1 || strings.HasPrefix(fields[0], "[") {
			continue
		}
		f.rules = append(f.rules, rule{pattern: fields[0], owners: fields[1:]})
	}
	return f
}

// Owners returns the owners of a path: those of the last matching pattern.
func (f *File) Owners(p string) []string {
	if f == nil {
		return nil
	}
	for i := len(f.rules) - 1; i >= 0; i-- {
		if match(f.rules[i].pattern, p) {
			return f.rules[i].owners
		}
	}
	return nil
}

// Slice returns the paths owned by login (matched as @login, or through any
// @org/team the caller lists in teams). ok is false when the file names the
// reviewer nowhere, in which case no narrowing applies.
func (f *File) Slice(paths []string, login string, teams []string) (mine []string, ok bool) {
	if f == nil {
		return nil, false
	}
	want := map[string]bool{"@" + strings.ToLower(login): true}
	for _, t := range teams {
		want[strings.ToLower(t)] = true
	}
	for _, p := range paths {
		for _, o := range f.Owners(p) {
			if want[strings.ToLower(o)] {
				mine = append(mine, p)
				break
			}
		}
	}
	return mine, len(mine) > 0
}

// match implements the common CODEOWNERS patterns: a leading slash anchors at
// the root, a trailing slash means a directory, a bare name matches at any
// depth, * matches within one segment and ** across segments.
func match(pattern, p string) bool {
	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	dir := strings.HasSuffix(pattern, "/")
	pattern = strings.TrimSuffix(pattern, "/")
	if pattern == "*" || pattern == "**" {
		return true
	}
	if !strings.Contains(pattern, "/") && !anchored {
		// A bare name matches that file or directory anywhere.
		segs := strings.Split(p, "/")
		for i, s := range segs {
			if ok, _ := path.Match(pattern, s); ok && (!dir || i < len(segs)-1) {
				return true
			}
		}
		return false
	}
	return globSegs(strings.Split(pattern, "/"), strings.Split(p, "/"), dir)
}

func globSegs(pat, segs []string, dirOnly bool) bool {
	if len(pat) == 0 {
		// The pattern is used up: it matched a directory prefix (fine) or the
		// whole path (fine unless the pattern names a directory).
		return len(segs) > 0 || !dirOnly
	}
	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if globSegs(pat[1:], segs[i:], dirOnly) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	if ok, _ := path.Match(pat[0], segs[0]); !ok {
		return false
	}
	return globSegs(pat[1:], segs[1:], dirOnly)
}
