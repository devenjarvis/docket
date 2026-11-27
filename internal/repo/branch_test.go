package repo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devenjarvis/docket/internal/model"
)

type branchFixture struct {
	origin string
	head   string
	pr     model.PR
	g      Git
	b      Branch
}

// newBranch makes an origin with a "feature" branch behind refs/pull/3/head
// and prepares a writable checkout of it.
func newBranch(t *testing.T) *branchFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "t")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "t@t")
	}
	origin := t.TempDir()
	run(t, origin, "init", "-q", "-b", "main")
	writeFile(t, origin, "a.txt", "one\n")
	run(t, origin, "add", "-A")
	run(t, origin, "commit", "-q", "-m", "base")
	run(t, origin, "checkout", "-q", "-b", "feature")
	writeFile(t, origin, "a.txt", "one\ntwo\n")
	run(t, origin, "add", "-A")
	run(t, origin, "commit", "-q", "-m", "change")
	head := run(t, origin, "rev-parse", "HEAD")
	run(t, origin, "update-ref", "refs/pull/3/head", head)
	run(t, origin, "checkout", "-q", "main")

	g := Git{Home: t.TempDir(), Remote: func(model.PR) string { return origin }}
	pr := model.PR{Owner: "o", Repo: "r", Number: 3, HeadSHA: head, BaseRef: "main", HeadRef: "feature"}
	b, err := g.PrepareBranch(context.Background(), pr, model.MergeState{HeadRepo: "o/r"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { g.ReleaseBranch(context.Background(), pr, b) })
	if b.Start != head {
		t.Fatalf("start = %s, want %s", b.Start, head)
	}
	return &branchFixture{origin: origin, head: head, pr: pr, g: g, b: b}
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commit makes a commit in the checkout and returns its SHA.
func (f *branchFixture) commit(t *testing.T, name, body string) string {
	t.Helper()
	writeFile(t, f.b.Work, name, body)
	run(t, f.b.Work, "add", "-A")
	run(t, f.b.Work, "commit", "-q", "-m", "fix "+name)
	return run(t, f.b.Work, "rev-parse", "HEAD")
}

func (f *branchFixture) originFeature(t *testing.T) string {
	return run(t, f.origin, "rev-parse", "refs/heads/feature")
}

func TestPushFastForwardsTheHeadBranch(t *testing.T) {
	f := newBranch(t)
	ctx := context.Background()
	fix := f.commit(t, "b.txt", "fixed\n")
	if err := f.g.Push(ctx, f.b, f.pr, f.b.Start, fix); err != nil {
		t.Fatal(err)
	}
	if got := f.originFeature(t); got != fix {
		t.Errorf("origin feature = %s, want the fix %s", got, fix)
	}
	p, err := f.g.Patch(ctx, f.b, f.b.Start, fix)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, "+fixed") || !strings.Contains(p, "b.txt") {
		t.Errorf("patch lacks the fix:\n%s", p)
	}
}

func TestPushFailsWhenTheBranchMoved(t *testing.T) {
	f := newBranch(t)
	run(t, f.origin, "checkout", "-q", "feature")
	writeFile(t, f.origin, "c.txt", "author pushed\n")
	run(t, f.origin, "add", "-A")
	run(t, f.origin, "commit", "-q", "-m", "author")
	moved := run(t, f.origin, "rev-parse", "HEAD")
	run(t, f.origin, "checkout", "-q", "main")

	fix := f.commit(t, "b.txt", "fixed\n")
	err := f.g.Push(context.Background(), f.b, f.pr, f.b.Start, fix)
	if !errors.Is(err, ErrMoved) {
		t.Fatalf("push err = %v, want ErrMoved", err)
	}
	if got := f.originFeature(t); got != moved {
		t.Errorf("origin feature = %s, want it left at %s", got, moved)
	}
}

func TestPushRefusesACommitNotOnTop(t *testing.T) {
	f := newBranch(t)
	ctx := context.Background()
	run(t, f.b.Work, "checkout", "-q", "--orphan", "stray")
	stray := f.commit(t, "z.txt", "unrelated\n")
	err := f.g.Push(ctx, f.b, f.pr, f.b.Start, stray)
	if !errors.Is(err, ErrNotOnTop) {
		t.Fatalf("push err = %v, want ErrNotOnTop", err)
	}
	if got := f.originFeature(t); got != f.head {
		t.Errorf("origin feature = %s, want it left at %s", got, f.head)
	}
}

func TestOnTop(t *testing.T) {
	f := newBranch(t)
	ctx := context.Background()
	fix := f.commit(t, "b.txt", "fixed\n")
	if !f.g.OnTop(ctx, f.b, fix) {
		t.Error("a descendant is not on top")
	}
	if !f.g.OnTop(ctx, f.b, f.b.Start) {
		t.Error("start itself is not on top")
	}
	run(t, f.b.Work, "checkout", "-q", "--orphan", "stray")
	stray := f.commit(t, "z.txt", "unrelated\n")
	if f.g.OnTop(ctx, f.b, stray) {
		t.Error("an unrelated commit is on top")
	}
}

func TestRevertUndoesACommit(t *testing.T) {
	f := newBranch(t)
	ctx := context.Background()
	first := f.commit(t, "b.txt", "fixed\n")
	second := f.commit(t, "a.txt", "one\ntwo\nthree\n")
	r, err := f.g.Revert(ctx, f.b, second, second)
	if err != nil {
		t.Fatal(err)
	}
	if r == second || !f.g.OnTop(ctx, f.b, r) {
		t.Fatalf("revert %s is not a new commit on top", r)
	}
	if got, want := run(t, f.b.Work, "rev-parse", r+"^{tree}"), run(t, f.b.Work, "rev-parse", first+"^{tree}"); got != want {
		t.Errorf("revert tree = %s, want the tree before the commit %s", got, want)
	}
	if parent := run(t, f.b.Work, "rev-parse", r+"^"); parent != second {
		t.Errorf("revert parent = %s, want the tip %s", parent, second)
	}
}

func TestDocketsGitIgnoresAGitFileTheAgentRewrote(t *testing.T) {
	f := newBranch(t)
	ctx := context.Background()
	fix := f.commit(t, "b.txt", "fixed\n")
	// The agent may write anywhere in the checkout, .git included.
	writeFile(t, f.b.Work, ".git", "gitdir: "+t.TempDir()+"\n")
	if !f.g.OnTop(ctx, f.b, fix) {
		t.Fatal("OnTop followed the rewritten .git file")
	}
	if p, err := f.g.Patch(ctx, f.b, f.b.Start, fix); err != nil || !strings.Contains(p, "+fixed") {
		t.Fatalf("patch = %q, %v", p, err)
	}
	if err := f.g.Push(ctx, f.b, f.pr, f.b.Start, fix); err != nil {
		t.Fatal(err)
	}
	if got := f.originFeature(t); got != fix {
		t.Errorf("origin feature = %s, want %s", got, fix)
	}
}

func TestPrepareBranchStartsFromACleanHead(t *testing.T) {
	f := newBranch(t)
	f.commit(t, "b.txt", "rejected\n")
	writeFile(t, f.b.Work, "loose.txt", "left over\n")
	b, err := f.g.PrepareBranch(context.Background(), f.pr, model.MergeState{HeadRepo: "o/r"})
	if err != nil {
		t.Fatal(err)
	}
	if got := run(t, b.Work, "rev-parse", "HEAD"); got != f.head {
		t.Errorf("HEAD = %s, want the pull request's head %s", got, f.head)
	}
	for _, name := range []string{"b.txt", "loose.txt"} {
		if _, err := os.Stat(filepath.Join(b.Work, name)); err == nil {
			t.Errorf("%s survived into the new checkout", name)
		}
	}
}

func TestPrepareBranchRefusesAHeadItCannotPushTo(t *testing.T) {
	f := newBranch(t)
	ctx := context.Background()
	pr := f.pr
	pr.Author = "me"
	if _, err := f.g.PrepareBranch(ctx, pr, model.MergeState{}); err == nil {
		t.Error("a pull request whose head repository is gone was prepared")
	}
	if _, err := f.g.PrepareBranch(ctx, pr, model.MergeState{HeadRepo: "someone/r"}); err == nil {
		t.Error("a fork that does not let maintainers push was prepared")
	}
	if _, err := f.g.PrepareBranch(ctx, pr, model.MergeState{HeadRepo: "me/r"}); err != nil {
		t.Errorf("the author's own fork: %v", err)
	}
}

func TestPatchShowsARenameAsADeleteAndAnAdd(t *testing.T) {
	f := newBranch(t)
	run(t, f.b.Work, "mv", "a.txt", "moved.txt")
	run(t, f.b.Work, "commit", "-q", "-m", "move")
	mv := run(t, f.b.Work, "rev-parse", "HEAD")
	p, err := f.g.Patch(context.Background(), f.b, f.b.Start, mv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, "deleted file mode") || !strings.Contains(p, "new file mode") {
		t.Errorf("the rename hides what it removed:\n%s", p)
	}
}

func TestResolveGivesAFullSHA(t *testing.T) {
	f := newBranch(t)
	fix := f.commit(t, "b.txt", "fixed\n")
	got, err := f.g.Resolve(context.Background(), f.b, "HEAD")
	if err != nil || got != fix {
		t.Errorf("Resolve(HEAD) = %q, %v; want %s", got, err, fix)
	}
	if _, err := f.g.Resolve(context.Background(), f.b, "no-such-ref"); err == nil {
		t.Error("a missing ref resolved")
	}
}

func TestPushErrorTellsAMovedBranchFromARefusal(t *testing.T) {
	moved := errors.New("git push: ! [rejected] x -> feature (fetch first)")
	if !errors.Is(pushError(moved), ErrMoved) {
		t.Error("a branch that moved was not reported as moved")
	}
	refused := errors.New("git push: ! [remote rejected] x -> feature (protected branch hook declined)")
	if errors.Is(pushError(refused), ErrMoved) {
		t.Error("a protected branch was reported as moved")
	}
}

func TestRevertOfAMerge(t *testing.T) {
	f := newBranch(t)
	writeFile(t, f.b.Work, "m.txt", "side\n")
	run(t, f.b.Work, "checkout", "-q", "-b", "side", f.b.Start)
	run(t, f.b.Work, "add", "-A")
	run(t, f.b.Work, "commit", "-q", "-m", "side")
	side := run(t, f.b.Work, "rev-parse", "HEAD")
	run(t, f.b.Work, "checkout", "-q", "-B", "docket/tend/3", f.b.Start)
	writeFile(t, f.b.Work, "n.txt", "main side\n")
	run(t, f.b.Work, "add", "-A")
	run(t, f.b.Work, "commit", "-q", "-m", "ours")
	run(t, f.b.Work, "merge", "-q", "--no-ff", "-m", "merge side", side)
	merge := run(t, f.b.Work, "rev-parse", "HEAD")
	rev, err := f.g.Revert(context.Background(), f.b, merge, merge)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.b.Work, "m.txt")); err == nil {
		t.Errorf("the revert %s kept the merged file", rev)
	}
}
