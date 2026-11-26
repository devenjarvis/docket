package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devenjarvis/docket/internal/model"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// A pull request is checked out twice, at its head and at the commit it
// forked from, even when the base branch has moved on since.
func TestPrepareChecksOutHeadAndMergeBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	origin := t.TempDir()
	run(t, origin, "init", "-q", "-b", "main")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(origin, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "one\ntwo\n")
	run(t, origin, "add", "-A")
	run(t, origin, "commit", "-q", "-m", "base")
	fork := run(t, origin, "rev-parse", "HEAD")
	run(t, origin, "checkout", "-q", "-b", "feature")
	write("a.txt", "one\nTWO\n")
	write("b.txt", "new\n")
	run(t, origin, "add", "-A")
	run(t, origin, "commit", "-q", "-m", "change")
	head := run(t, origin, "rev-parse", "HEAD")
	run(t, origin, "update-ref", "refs/pull/7/head", head)
	run(t, origin, "checkout", "-q", "main")
	write("c.txt", "main moved on\n")
	run(t, origin, "add", "-A")
	run(t, origin, "commit", "-q", "-m", "later on main")

	home := t.TempDir()
	g := Git{Home: home, Remote: func(model.PR) string { return origin }}
	pr := model.PR{Owner: "o", Repo: "r", Number: 7, HeadSHA: head, BaseRef: "main"}
	ws, d, err := g.Prepare(context.Background(), pr)
	if err != nil {
		t.Fatal(err)
	}
	if ws.MergeBase != fork {
		t.Errorf("merge base = %s, want the fork point %s", ws.MergeBase, fork)
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
	if got := read(filepath.Join(ws.Head, "a.txt")); got != "one\nTWO\n" {
		t.Errorf("head checkout has %q", got)
	}
	if got := read(filepath.Join(ws.Base, "a.txt")); got != "one\ntwo\n" {
		t.Errorf("base checkout has %q", got)
	}
	if _, err := os.Stat(filepath.Join(ws.Base, "c.txt")); err == nil {
		t.Error("base checkout includes work that landed on main after the fork")
	}
	if !strings.Contains(d, "+TWO") || !strings.Contains(d, "b.txt") || strings.Contains(d, "c.txt") {
		t.Errorf("diff is not base-to-head only:\n%s", d)
	}
	// A second call (a re-read) reuses the checkouts.
	if _, _, err := g.Prepare(context.Background(), pr); err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	g.Release(context.Background(), pr, ws)
	if _, err := os.Stat(ws.Head); err == nil {
		t.Error("release left the head checkout behind")
	}
}
