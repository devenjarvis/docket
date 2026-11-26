// Package diff parses a unified git diff into files, hunks and numbered
// lines, and cuts it down to the line ranges a brief points at.
package diff

import (
	"regexp"
	"strconv"
	"strings"
)

type File struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath,omitempty"`
	Status    string `json:"status"` // added, deleted, modified, renamed
	Binary    bool   `json:"binary,omitempty"`
	Hunks     []Hunk `json:"hunks"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type Hunk struct {
	OldStart int    `json:"oldStart"`
	NewStart int    `json:"newStart"`
	Header   string `json:"header,omitempty"`
	Lines    []Line `json:"lines"`
}

// Line kinds are " " (context), "+" and "-". Old and New are 1-based line
// numbers, zero on the side the line does not exist on.
type Line struct {
	Kind string `json:"k"`
	Old  int    `json:"o,omitempty"`
	New  int    `json:"n,omitempty"`
	Text string `json:"t"`
	// Hot is set by Mark for lines a decision points at.
	Hot bool `json:"hot,omitempty"`
}

var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

// Parse reads the output of `git diff`.
func Parse(text string) []File {
	var files []File
	var cur *File
	var hunk *Hunk
	var oldN, newN int
	flush := func() {
		if cur != nil {
			if hunk != nil {
				cur.Hunks = append(cur.Hunks, *hunk)
				hunk = nil
			}
			files = append(files, *cur)
			cur = nil
		}
	}
	for _, ln := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(ln, "diff --git "):
			flush()
			cur = &File{Status: "modified"}
			// "diff --git a/x b/y": take the b side as a first guess.
			if i := strings.LastIndex(ln, " b/"); i >= 0 {
				cur.Path = ln[i+3:]
			}
		case cur == nil:
			continue
		case hunk == nil && strings.HasPrefix(ln, "new file mode"):
			cur.Status = "added"
		case hunk == nil && strings.HasPrefix(ln, "deleted file mode"):
			cur.Status = "deleted"
		case hunk == nil && strings.HasPrefix(ln, "rename from "):
			cur.Status = "renamed"
			cur.OldPath = strings.TrimPrefix(ln, "rename from ")
		case hunk == nil && strings.HasPrefix(ln, "rename to "):
			cur.Path = strings.TrimPrefix(ln, "rename to ")
		case hunk == nil && strings.HasPrefix(ln, "Binary files "):
			cur.Binary = true
		case hunk == nil && strings.HasPrefix(ln, "--- "):
			if p := strings.TrimPrefix(ln, "--- a/"); p != ln && cur.Status == "deleted" {
				cur.Path = p
			}
		case hunk == nil && strings.HasPrefix(ln, "+++ "):
			if p := strings.TrimPrefix(ln, "+++ b/"); p != ln {
				cur.Path = p
			}
		case strings.HasPrefix(ln, "@@ "):
			m := hunkRe.FindStringSubmatch(ln)
			if m == nil {
				continue
			}
			if hunk != nil {
				cur.Hunks = append(cur.Hunks, *hunk)
			}
			oldN, _ = strconv.Atoi(m[1])
			newN, _ = strconv.Atoi(m[3])
			hunk = &Hunk{OldStart: oldN, NewStart: newN, Header: m[5]}
		case hunk != nil && strings.HasPrefix(ln, "+"):
			hunk.Lines = append(hunk.Lines, Line{Kind: "+", New: newN, Text: ln[1:]})
			newN++
			cur.Additions++
		case hunk != nil && strings.HasPrefix(ln, "-"):
			hunk.Lines = append(hunk.Lines, Line{Kind: "-", Old: oldN, Text: ln[1:]})
			oldN++
			cur.Deletions++
		case hunk != nil && strings.HasPrefix(ln, " "):
			hunk.Lines = append(hunk.Lines, Line{Kind: " ", Old: oldN, New: newN, Text: ln[1:]})
			oldN++
			newN++
		case hunk != nil && strings.HasPrefix(ln, `\`):
			// "\ No newline at end of file"
		case hunk != nil && ln == "":
			// A blank context line some tools emit without its leading space;
			// the trailing split element also lands here and is harmless.
		}
	}
	flush()
	return files
}

// Find returns the file with that path.
func Find(files []File, path string) (File, bool) {
	for _, f := range files {
		if f.Path == path || (f.OldPath != "" && f.OldPath == path) {
			return f, true
		}
	}
	return File{}, false
}

// position is where a line sits on the new side: its own number for context
// and added lines, and for a removed line the number of the next new line.
func positions(h Hunk) []int {
	pos := make([]int, len(h.Lines))
	next := 0
	for i := len(h.Lines) - 1; i >= 0; i-- {
		if h.Lines[i].New > 0 {
			next = h.Lines[i].New
		}
		pos[i] = next
	}
	// Removed lines at the very end of a hunk have nothing after them.
	last := h.NewStart
	for i, l := range h.Lines {
		if l.New > 0 {
			last = l.New
		}
		if pos[i] == 0 {
			pos[i] = last
		}
	}
	return pos
}

func inRanges(n int, ranges [][2]int) bool {
	for _, r := range ranges {
		if n >= r[0] && n <= r[1] {
			return true
		}
	}
	return false
}

// Slice returns the parts of f's hunks that fall within ranges (new-side line
// numbers), with up to ctx lines around each. Empty ranges returns every hunk.
func Slice(f File, ranges [][2]int, ctx int) []Hunk {
	if len(ranges) == 0 {
		return f.Hunks
	}
	var out []Hunk
	for _, h := range f.Hunks {
		pos := positions(h)
		keep := make([]bool, len(h.Lines))
		any := false
		for i, l := range h.Lines {
			if l.Kind != " " && inRanges(pos[i], ranges) || l.Kind == " " && inRanges(l.New, ranges) {
				any = true
				for j := max(0, i-ctx); j <= min(len(h.Lines)-1, i+ctx); j++ {
					keep[j] = true
				}
			}
		}
		if !any {
			continue
		}
		var cur *Hunk
		for i, l := range h.Lines {
			if !keep[i] {
				if cur != nil {
					out = append(out, *cur)
					cur = nil
				}
				continue
			}
			if cur == nil {
				cur = &Hunk{OldStart: l.Old, NewStart: pos[i], Header: h.Header}
			}
			cur.Lines = append(cur.Lines, l)
		}
		if cur != nil {
			out = append(out, *cur)
		}
	}
	return out
}

// Mark flags the changed lines of hunks that fall within ranges.
func Mark(hunks []Hunk, ranges [][2]int) []Hunk {
	out := make([]Hunk, len(hunks))
	for i, h := range hunks {
		pos := positions(h)
		nh := h
		nh.Lines = make([]Line, len(h.Lines))
		for j, l := range h.Lines {
			if l.Kind != " " && inRanges(pos[j], ranges) {
				l.Hot = true
			}
			nh.Lines[j] = l
		}
		out[i] = nh
	}
	return out
}

// Changed counts added and removed lines in hunks.
func Changed(hunks []Hunk) int {
	n := 0
	for _, h := range hunks {
		for _, l := range h.Lines {
			if l.Kind != " " {
				n++
			}
		}
	}
	return n
}

// HasNewLine reports whether line n exists on the new side of f's diff, which
// is what GitHub needs to attach a comment there.
func HasNewLine(f File, n int) bool {
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			if l.New == n {
				return true
			}
		}
	}
	return false
}
