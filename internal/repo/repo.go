// Package repo keeps a local copy of each repository Docket reads and cuts
// two throwaway checkouts per pull request: the head, and the commit it
// branched from. The agent runs code in both, which is how a before/after
// comparison gets measured instead of guessed.
package repo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/devenjarvis/docket/internal/model"
)

// Workspace is one review's directories. Everything lives under Dir.
type Workspace struct {
	Dir     string `json:"dir"`
	Head    string `json:"head"`    // checkout of the PR's head
	Base    string `json:"base"`    // checkout of the merge base
	Scratch string `json:"scratch"` // the agent's working files
	Out     string `json:"out"`     // what the agent hands back
	// MergeBase is the commit Base is checked out at.
	MergeBase string `json:"mergeBase"`
}

// Preparer makes a Workspace for a PR and returns its diff.
type Preparer interface {
	Prepare(ctx context.Context, pr model.PR) (Workspace, string, error)
	// Release removes a workspace's checkouts.
	Release(ctx context.Context, pr model.PR, ws Workspace)
}

// Git is the real Preparer.
type Git struct {
	// Home is Docket's data directory.
	Home string
	// Remote returns the URL to clone a repository from; nil means GitHub
	// over HTTPS.
	Remote func(pr model.PR) string
}

// Layout returns the directories for a PR at a head commit, without creating
// them.
func Layout(home string, pr model.PR) Workspace {
	sha := pr.HeadSHA
	if len(sha) > 10 {
		sha = sha[:10]
	}
	dir := filepath.Join(home, "reviews", pr.Owner, pr.Repo, fmt.Sprint(pr.Number), sha)
	return Workspace{
		Dir: dir, Head: filepath.Join(dir, "head"), Base: filepath.Join(dir, "base"),
		Scratch: filepath.Join(dir, "scratch"), Out: filepath.Join(dir, "out"),
	}
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	// gh hands git a token for private repositories without the user having
	// to run `gh auth setup-git`.
	full := append([]string{
		"-c", "credential.helper=",
		"-c", "credential.helper=!gh auth git-credential",
		"-c", "advice.detachedHead=false",
	}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}

func (g Git) mirror(pr model.PR) string {
	return filepath.Join(g.Home, "repos", pr.Owner, pr.Repo+".git")
}

// sync makes sure the PR's repository has a local mirror and fetches the
// PR's head into refs/docket/<number>/head, along with any extra refspecs.
// It returns the mirror's path and the head ref.
func (g Git) sync(ctx context.Context, pr model.PR, extra ...string) (string, string, error) {
	mirror := g.mirror(pr)
	if _, err := os.Stat(filepath.Join(mirror, "HEAD")); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(mirror), 0o755); err != nil {
			return mirror, "", err
		}
		url := fmt.Sprintf("https://github.com/%s/%s.git", pr.Owner, pr.Repo)
		if g.Remote != nil {
			url = g.Remote(pr)
		}
		// Blobs arrive when a checkout needs them, so a large repository
		// costs only its history up front.
		if _, err := git(ctx, "", "clone", "--bare", "--filter=blob:none", url, mirror); err != nil {
			return mirror, "", err
		}
	}
	headRef := fmt.Sprintf("refs/docket/%d/head", pr.Number)
	args := append([]string{"fetch", "--force", "origin",
		fmt.Sprintf("+refs/pull/%d/head:%s", pr.Number, headRef)}, extra...)
	if _, err := git(ctx, mirror, args...); err != nil {
		return mirror, "", err
	}
	return mirror, headRef, nil
}

func (g Git) Prepare(ctx context.Context, pr model.PR) (Workspace, string, error) {
	ws := Layout(g.Home, pr)
	baseRef := fmt.Sprintf("refs/docket/%d/base", pr.Number)
	mirror, headRef, err := g.sync(ctx, pr, fmt.Sprintf("+refs/heads/%s:%s", pr.BaseRef, baseRef))
	if err != nil {
		return ws, "", err
	}
	head := pr.HeadSHA
	if head == "" {
		head = headRef
	}
	mb, err := git(ctx, mirror, "merge-base", head, baseRef)
	if err != nil {
		return ws, "", err
	}
	ws.MergeBase = strings.TrimSpace(mb)

	for _, d := range []string{ws.Scratch, ws.Out} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return ws, "", err
		}
	}
	for _, co := range []struct{ dir, rev string }{{ws.Head, head}, {ws.Base, ws.MergeBase}} {
		if _, err := os.Stat(filepath.Join(co.dir, ".git")); err == nil {
			continue
		}
		_, _ = git(ctx, mirror, "worktree", "prune")
		if _, err := git(ctx, mirror, "worktree", "add", "--force", "--detach", co.dir, co.rev); err != nil {
			return ws, "", err
		}
	}
	d, err := git(ctx, mirror, "diff", "--no-color", "--no-ext-diff", "-M", ws.MergeBase, head)
	if err != nil {
		return ws, "", err
	}
	return ws, d, nil
}

func (g Git) Release(ctx context.Context, pr model.PR, ws Workspace) {
	mirror := g.mirror(pr)
	for _, d := range []string{ws.Head, ws.Base} {
		_, _ = git(ctx, mirror, "worktree", "remove", "--force", d)
	}
	_ = os.RemoveAll(ws.Scratch)
	_, _ = git(ctx, mirror, "worktree", "prune")
}
