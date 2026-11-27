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
	// GitDir is the worktree's admin directory inside the mirror, read when
	// the worktree was made. Docket's own git uses it instead of Work/.git,
	// which the agent can write.
	GitDir string `json:"gitDir"`
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
	// Resolve turns what the agent named a commit into its full SHA.
	Resolve(ctx context.Context, b Branch, ref string) (string, error)
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
// committed to. Each call starts from a clean checkout of the head, so a run
// never builds on what an earlier run left.
func (g Git) PrepareBranch(ctx context.Context, pr model.PR, st model.MergeState) (Branch, error) {
	b := BranchLayout(g.Home, pr)
	head := pr
	owner, name, ok := strings.Cut(st.HeadRepo, "/")
	if !ok || owner == "" || name == "" {
		return b, errors.New("the pull request's branch is in a repository that no longer exists")
	}
	if st.HeadRepo != pr.Owner+"/"+pr.Repo && !st.MaintainerCanModify && owner != pr.Author {
		return b, fmt.Errorf("the branch is in %s, which does not let you push to it", st.HeadRepo)
	}
	head.Owner, head.Repo = owner, name
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
	_, _ = git(ctx, mirror, "worktree", "remove", "--force", b.Work)
	if err := os.RemoveAll(b.Work); err != nil {
		return b, err
	}
	_, _ = git(ctx, mirror, "worktree", "prune")
	if _, err := git(ctx, mirror, "worktree", "add", "--force", "-B", localBranch(pr), b.Work, pr.HeadSHA); err != nil {
		return b, err
	}
	dotgit, err := os.ReadFile(filepath.Join(b.Work, ".git"))
	if err != nil {
		return b, err
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(dotgit)), "gitdir:"))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(b.Work, gitDir)
	}
	admin, err := filepath.EvalSymlinks(filepath.Join(mirror, "worktrees"))
	if err != nil {
		return b, err
	}
	if real, err := filepath.EvalSymlinks(gitDir); err != nil || !strings.HasPrefix(real, admin+string(filepath.Separator)) {
		return b, fmt.Errorf("git made the worktree's admin directory outside the mirror: %s", gitDir)
	}
	b.GitDir = gitDir
	return b, nil
}

// git runs Docket's own git in the checkout. It names the admin directory and
// work tree itself and turns hooks and fsmonitor off, so nothing the agent
// wrote into the checkout decides what git runs.
func (b Branch) git(ctx context.Context, args ...string) (string, error) {
	if b.GitDir == "" {
		return "", errors.New("this checkout is from an older Docket: run the agent again")
	}
	return gitWith(ctx, b.Work, []string{
		"--git-dir=" + b.GitDir, "--work-tree=" + b.Work,
		"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.quotePath=false",
	}, args...)
}

func (g Git) OnTop(ctx context.Context, b Branch, sha string) bool {
	_, err := b.git(ctx, "merge-base", "--is-ancestor", b.Start, sha)
	return err == nil
}

// Patch shows renames as a delete and an add, so a file moved away is judged
// as a file removed.
func (g Git) Patch(ctx context.Context, b Branch, from, to string) (string, error) {
	return b.git(ctx, "diff", "--no-color", "--no-ext-diff", "--no-renames", from, to)
}

func (g Git) Resolve(ctx context.Context, b Branch, ref string) (string, error) {
	if strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("%q is not a commit", ref)
	}
	out, err := b.git(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%q is not a commit in the checkout", ref)
	}
	return strings.TrimSpace(out), nil
}

func (g Git) Push(ctx context.Context, b Branch, pr model.PR, from, to string) error {
	if _, err := b.git(ctx, "merge-base", "--is-ancestor", from, to); err != nil {
		return fmt.Errorf("%w: %s is not on top of %s", ErrNotOnTop, to, from)
	}
	if pr.HeadRef == "" {
		return errors.New("the pull request has no head branch")
	}
	_, err := b.git(ctx, "push", b.Remote, to+":refs/heads/"+pr.HeadRef)
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
	if _, err := b.git(ctx, "reset", "-q", "--hard", tip); err != nil {
		return "", err
	}
	if _, err := b.git(ctx, "revert", "--no-edit", sha); err != nil {
		_, _ = b.git(ctx, "revert", "--abort")
		return "", err
	}
	out, err := b.git(ctx, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

func (g Git) ReleaseBranch(ctx context.Context, pr model.PR, b Branch) {
	mirror := g.mirror(pr)
	_, _ = git(ctx, mirror, "worktree", "remove", "--force", b.Work)
	_ = os.RemoveAll(b.Scratch)
	_, _ = git(ctx, mirror, "worktree", "prune")
}
