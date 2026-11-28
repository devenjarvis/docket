// Package gh talks to GitHub through the user's own `gh` CLI, so Docket holds
// no token of its own and works wherever `gh` is signed in.
package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/model"
)

// Client is what Docket needs from GitHub.
type Client interface {
	// Viewer returns the signed-in login.
	Viewer(ctx context.Context) (string, error)
	// Assigned lists open PRs waiting on the viewer: review requested
	// (directly or through a team) or assigned. Only the search fields are
	// filled in; call PR for the rest.
	Assigned(ctx context.Context) ([]model.PR, error)
	// PR returns one pull request in full.
	PR(ctx context.Context, owner, repo string, number int) (model.PR, error)
	// PostReview submits a review and returns its URL.
	PostReview(ctx context.Context, pr model.PR, in ReviewInput) (string, error)
}

// ReviewInput is one review as GitHub's API takes it.
type ReviewInput struct {
	CommitID string          `json:"commit_id"`
	Event    string          `json:"event"` // APPROVE, REQUEST_CHANGES, COMMENT
	Body     string          `json:"body"`
	Comments []ReviewComment `json:"comments,omitempty"`
}

type ReviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side"`
	Body string `json:"body"`
}

// CLI is the real Client.
type CLI struct {
	// Bin is the gh binary; empty means "gh" on PATH.
	Bin string
}

func (c CLI) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "gh"
}

// ErrNoGH means the gh CLI is missing or not signed in.
var ErrNoGH = errors.New("the GitHub CLI (gh) is not installed or not signed in: run `gh auth login`")

func (c CLI) api(ctx context.Context, stdin []byte, out any, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.bin(), append([]string{"api"}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if err := cmd.Run(); err != nil {
		var ee *exec.Error
		if errors.As(err, &ee) {
			return ErrNoGH
		}
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(msg, "gh auth login") || strings.Contains(msg, "not logged in") {
			return ErrNoGH
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("gh api %s: %s", args[len(args)-1], msg)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(stdout.Bytes(), out)
}

func (c CLI) Viewer(ctx context.Context) (string, error) {
	var u struct {
		Login string `json:"login"`
	}
	if err := c.api(ctx, nil, &u, "user"); err != nil {
		return "", err
	}
	return u.Login, nil
}

type searchItem struct {
	Number        int       `json:"number"`
	Title         string    `json:"title"`
	HTMLURL       string    `json:"html_url"`
	RepositoryURL string    `json:"repository_url"`
	Draft         bool      `json:"draft"`
	UpdatedAt     time.Time `json:"updated_at"`
	User          struct {
		Login string `json:"login"`
	} `json:"user"`
}

func (c CLI) search(ctx context.Context, q string) ([]searchItem, error) {
	var res struct {
		Items []searchItem `json:"items"`
	}
	err := c.api(ctx, nil, &res, "-X", "GET", "search/issues", "-f", "q="+q, "-f", "per_page=100", "-f", "sort=updated")
	return res.Items, err
}

func (c CLI) Assigned(ctx context.Context) ([]model.PR, error) {
	queries := []struct{ q, why string }{
		{"is:pr is:open archived:false review-requested:@me", "review requested"},
		{"is:pr is:open archived:false assignee:@me -author:@me", "assigned"},
	}
	byKey := map[string]*model.PR{}
	var order []string
	for _, qq := range queries {
		items, err := c.search(ctx, qq.q)
		if err != nil {
			return nil, err
		}
		for _, found := range searchPRs(items, qq.why) {
			k := found.Key()
			if p, ok := byKey[k]; ok {
				p.Why = append(p.Why, qq.why)
				continue
			}
			byKey[k] = &found
			order = append(order, k)
		}
	}
	out := make([]model.PR, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out, nil
}

func (c CLI) PR(ctx context.Context, owner, repo string, number int) (model.PR, error) {
	var p struct {
		Number    int       `json:"number"`
		Title     string    `json:"title"`
		Body      string    `json:"body"`
		HTMLURL   string    `json:"html_url"`
		Draft     bool      `json:"draft"`
		State     string    `json:"state"`
		Merged    bool      `json:"merged"`
		Additions int       `json:"additions"`
		Deletions int       `json:"deletions"`
		Changed   int       `json:"changed_files"`
		UpdatedAt time.Time `json:"updated_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if err := c.api(ctx, nil, &p, fmt.Sprintf("repos/%s/%s/pulls/%d", owner, repo, number)); err != nil {
		return model.PR{}, err
	}
	state := p.State
	if p.Merged {
		state = "merged"
	}
	return model.PR{
		Owner: owner, Repo: repo, Number: p.Number, Title: p.Title, Body: p.Body,
		Author: p.User.Login, URL: p.HTMLURL, Draft: p.Draft, State: state, UpdatedAt: p.UpdatedAt,
		HeadSHA: p.Head.SHA, HeadRef: p.Head.Ref, BaseSHA: p.Base.SHA, BaseRef: p.Base.Ref,
		Additions: p.Additions, Deletions: p.Deletions, Files: p.Changed,
	}, nil
}

func (c CLI) PostReview(ctx context.Context, pr model.PR, in ReviewInput) (string, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	var res struct {
		HTMLURL string `json:"html_url"`
	}
	path := fmt.Sprintf("repos/%s/%s/pulls/%d/reviews", pr.Owner, pr.Repo, pr.Number)
	if err := c.api(ctx, body, &res, "-X", "POST", "--input", "-", path); err != nil {
		return "", err
	}
	return res.HTMLURL, nil
}
