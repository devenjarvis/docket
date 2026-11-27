package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devenjarvis/docket/internal/model"
)

// Branch is a writable checkout of one pull request's head branch, where an
// agent commits fixes for the author.
type Branch struct {
	Dir     string `json:"dir"`  // everything lives under Dir
	Work    string `json:"work"` // the worktree, on a local branch at Start
	Scratch string `json:"scratch"`
	Out     string `json:"out"`
	Start   string `json:"start"`  // the head SHA the checkout began at
	Remote  string `json:"remote"` // URL pushes go to: the head repository
}

// ErrMoved means the branch on GitHub moved since Start: someone pushed.
var ErrMoved = errors.New("the pull request's branch moved: someone pushed to it")

// ErrNotOnTop means a commit does not descend from the checkout's start.
var ErrNotOnTop = errors.New("the commit does not build on the branch's head")

// Brancher is what the tend job needs from git. Git implements it; the demo
// implements a fake.
type Brancher interface {
	PrepareBranch(ctx context.Context, pr model.PR, st model.MergeState) (Branch, error)
	// OnTop reports whether sha is Start or descends from it.
	OnTop(ctx context.Context, b Branch, sha string) bool
	// Patch is the diff from..to, for showing and judging a fix.
	Patch(ctx context.Context, b Branch, from, to string) (string, error)
	// Push makes the head branch on GitHub point at to. It is only ever a
	// fast-forward from from: never a force push.
	Push(ctx context.Context, b Branch, pr model.PR, from, to string) error
	// Revert commits the revert of sha on top of tip and returns the new commit.
	Revert(ctx context.Context, b Branch, tip, sha string) (string, error)
	ReleaseBranch(ctx context.Context, pr model.PR, b Branch)
}

var _ Brancher = Git{}

// BranchLayout returns the directories for tending a PR at a head commit,
// without creating them.
func BranchLayout(home string, pr model.PR) Branch {
	sha := pr.HeadSHA
	if len(sha) > 10 {
		sha = sha[:10]
	}
	dir := filepath.Join(home, "tend", pr.Owner, pr.Repo, fmt.Sprint(pr.Number), sha)
	return Branch{
		Dir: dir, Work: filepath.Join(dir, "work"),
		Scratch: filepath.Join(dir, "scratch"), Out: filepath.Join(dir, "out"),
		Start: pr.HeadSHA,
	}
}

func localBranch(pr model.PR) string { return fmt.Sprintf("docket/tend/%d", pr.Number) }

// PrepareBranch checks out the PR's head on a local branch that fixes are
// committed to.
func (g Git) PrepareBranch(ctx context.Context, pr model.PR, st model.MergeState) (Branch, error) {
	b := BranchLayout(g.Home, pr)
	head := pr
	if owner, name, ok := strings.Cut(st.HeadRepo, "/"); ok && owner != "" && name != "" {
		head.Owner, head.Repo = owner, name
	}
	b.Remote = fmt.Sprintf("https://github.com/%s/%s.git", head.Owner, head.Repo)
	if g.Remote != nil {
		b.Remote = g.Remote(head)
	}
	if pr.HeadSHA == "" {
		return b, errors.New("the pull request has no head commit")
	}
	mirror, _, err := g.sync(ctx, pr)
	if err != nil {
		return b, err
	}
	for _, d := range []string{b.Scratch, b.Out} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return b, err
		}
	}
	if _, err := os.Stat(filepath.Join(b.Work, ".git")); err == nil {
		return b, nil
	}
	_, _ = git(ctx, mirror, "worktree", "prune")
	if _, err := git(ctx, mirror, "worktree", "add", "--force", "-B", localBranch(pr), b.Work, pr.HeadSHA); err != nil {
		return b, err
	}
	return b, nil
}

func (g Git) OnTop(ctx context.Context, b Branch, sha string) bool {
	_, err := git(ctx, b.Work, "merge-base", "--is-ancestor", b.Start, sha)
	return err == nil
}

func (g Git) Patch(ctx context.Context, b Branch, from, to string) (string, error) {
	return git(ctx, b.Work, "diff", "--no-color", "--no-ext-diff", "-M", from, to)
}

func (g Git) Push(ctx context.Context, b Branch, pr model.PR, from, to string) error {
	if _, err := git(ctx, b.Work, "merge-base", "--is-ancestor", from, to); err != nil {
		return fmt.Errorf("%w: %s is not on top of %s", ErrNotOnTop, to, from)
	}
	if pr.HeadRef == "" {
		return errors.New("the pull request has no head branch")
	}
	_, err := git(ctx, b.Work, "push", b.Remote, to+":refs/heads/"+pr.HeadRef)
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, s := range []string{"rejected", "non-fast-forward", "fetch first"} {
		if strings.Contains(msg, s) {
			return fmt.Errorf("%w: %v", ErrMoved, err)
		}
	}
	return err
}

func (g Git) Revert(ctx context.Context, b Branch, tip, sha string) (string, error) {
	if _, err := git(ctx, b.Work, "reset", "-q", "--hard", tip); err != nil {
		return "", err
	}
	if _, err := git(ctx, b.Work, "revert", "--no-edit", sha); err != nil {
		_, _ = git(ctx, b.Work, "revert", "--abort")
		return "", err
	}
	out, err := git(ctx, b.Work, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

func (g Git) ReleaseBranch(ctx context.Context, pr model.PR, b Branch) {
	mirror := g.mirror(pr)
	_, _ = git(ctx, mirror, "worktree", "remove", "--force", b.Work)
	_ = os.RemoveAll(b.Scratch)
	_, _ = git(ctx, mirror, "worktree", "prune")
}
