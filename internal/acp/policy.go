package acp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Policy is the set of directories an agent session may touch through the
// client's fs/* and terminal/* methods. It is policy enforcement in Docket, not
// an OS sandbox: it bounds what Docket itself will read, write or start on the
// agent's behalf. An agent that ignores the client methods and uses its own
// tools is not bound by it (see Client.Isolation).
//
// Every root must be an absolute path. A request is allowed when its path,
// with symlinks resolved, is a root or lies under one.
type Policy struct {
	// Job names the job the policy is for ("ask", "verify", …); it appears in
	// rejection messages so the agent knows why.
	Job string
	// Read lists the roots fs/read_text_file may read. Write roots are not
	// readable unless listed here too.
	Read []string
	// Write lists the roots fs/write_text_file may create or overwrite.
	Write []string
	// Exec lists the roots terminal/create may use as a working directory.
	// Empty means no commands at all.
	Exec []string
}

// ReviewPolicy is for reading a pull request: read anything in the review
// directory (the head and base checkouts, the rulebook copy, earlier output),
// write only the out and scratch directories, and run commands in the
// checkouts and scratch. The checkouts are throwaway worktrees, so a command
// that writes into one harms nothing the user keeps.
func ReviewPolicy(reviewDir, headDir, baseDir, scratchDir, outDir string) *Policy {
	return &Policy{
		Job:   "review",
		Read:  []string{reviewDir},
		Write: []string{scratchDir, outDir},
		Exec:  []string{headDir, baseDir, scratchDir},
	}
}

// AskPolicy is for a follow-up question about a review: the same reach as
// ReviewPolicy, so the agent can answer with a run rather than a guess.
func AskPolicy(reviewDir, headDir, baseDir, scratchDir, outDir string) *Policy {
	p := ReviewPolicy(reviewDir, headDir, baseDir, scratchDir, outDir)
	p.Job = "ask"
	return p
}

// ErrOutsidePolicy is wrapped by every rejection, so callers can tell a
// policy refusal from an I/O error.
var ErrOutsidePolicy = errors.New("outside this job's file policy")

// CheckRead returns the resolved path to read, or an error if p does not
// allow reading path.
func (p *Policy) CheckRead(path string) (string, error) {
	if p == nil {
		return p.check("read", path, nil)
	}
	return p.check("read", path, p.Read)
}

// CheckWrite returns the resolved path to write, or an error if p does not
// allow writing path. The file need not exist yet.
func (p *Policy) CheckWrite(path string) (string, error) {
	if p == nil {
		return p.check("write", path, nil)
	}
	return p.check("write", path, p.Write)
}

// CheckExec returns the resolved working directory for a command, or an error
// if p does not allow running commands there. An empty dir means the first
// exec root.
func (p *Policy) CheckExec(dir string) (string, error) {
	if p == nil || len(p.Exec) == 0 {
		return "", fmt.Errorf("%w: the %s job may not run commands", ErrOutsidePolicy, p.job())
	}
	if dir == "" {
		dir = p.Exec[0]
	}
	return p.check("run commands in", dir, p.Exec)
}

func (p *Policy) job() string {
	if p == nil || p.Job == "" {
		return "current"
	}
	return p.Job
}

func (p *Policy) check(verb, path string, roots []string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("%w: docket has no file policy for this session, so it may not %s %s", ErrOutsidePolicy, verb, path)
	}
	if len(roots) == 0 {
		return "", fmt.Errorf("%w: the %s job may not %s any files (%s)", ErrOutsidePolicy, p.job(), verb, path)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: path must be absolute: %q", ErrOutsidePolicy, path)
	}
	resolved, err := resolve(path)
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		r, err := resolve(root)
		if err != nil {
			continue
		}
		if within(r, resolved) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("%w: the %s job may %s only under %s, not %s",
		ErrOutsidePolicy, p.job(), verb, strings.Join(roots, ", "), path)
}

func within(root, path string) bool {
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// resolve cleans an absolute path and resolves every symlink in it, so a
// link inside an allowed root that points outside it is judged by its
// target. Components that do not exist yet (a file about to be written) are
// kept as they are, under the deepest existing ancestor's real path.
func resolve(path string) (string, error) {
	path = filepath.Clean(path)
	var rest []string
	cur := path
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			parts := append([]string{real}, rest...)
			return filepath.Join(parts...), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if _, lerr := os.Lstat(cur); lerr == nil {
			// A dangling symlink: writing through it would land wherever it
			// points, so refuse rather than guess.
			return "", fmt.Errorf("%w: %s is a symlink to a missing target", ErrOutsidePolicy, cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path, nil
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
}
