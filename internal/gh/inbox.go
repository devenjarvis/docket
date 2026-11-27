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

// Inbox is what Docket needs from GitHub to keep the user's own pull
// requests and every PR's conversation, and to act on them.
type Inbox interface {
	// Mine lists the viewer's open pull requests. Only the search fields are
	// filled in; call Client.PR for the rest.
	Mine(ctx context.Context) ([]model.PR, error)
	// Detail returns whether a pull request can merge, its checks, and what
	// people have said on it.
	Detail(ctx context.Context, owner, repo string, number int) (model.MergeState, []model.Check, model.Conversation, error)
	Reply(ctx context.Context, pr model.PR, threadID, body string) error
	Resolve(ctx context.Context, pr model.PR, threadID string, resolved bool) error
	Comment(ctx context.Context, pr model.PR, body string) error
	// Merge merges at sha, so a push that lands meanwhile is not merged
	// unseen. Method is MERGE, SQUASH or REBASE.
	Merge(ctx context.Context, pr model.PR, method, sha string) error
	AutoMerge(ctx context.Context, pr model.PR, nodeID, method string, on bool) error
	UpdateBranch(ctx context.Context, pr model.PR) error
	MarkReady(ctx context.Context, pr model.PR, nodeID string) error
	RequestReview(ctx context.Context, pr model.PR, logins []string) error
	// Rerun re-runs the failed jobs of one GitHub Actions run.
	Rerun(ctx context.Context, pr model.PR, runID int64) error
	// FailedLog returns the end of one Actions job's log.
	FailedLog(ctx context.Context, pr model.PR, jobID int64) (string, error)
}

var _ Inbox = CLI{}

func searchPRs(items []searchItem, why string) []model.PR {
	var out []model.PR
	for _, it := range items {
		// repository_url ends in /repos/<owner>/<repo>.
		parts := strings.Split(strings.TrimSuffix(it.RepositoryURL, "/"), "/")
		if len(parts) < 2 {
			continue
		}
		out = append(out, model.PR{
			Owner: parts[len(parts)-2], Repo: parts[len(parts)-1], Number: it.Number, Title: it.Title,
			Author: it.User.Login, URL: it.HTMLURL, Draft: it.Draft, UpdatedAt: it.UpdatedAt, Why: []string{why},
		})
	}
	return out
}

func (c CLI) Mine(ctx context.Context) ([]model.PR, error) {
	items, err := c.search(ctx, "is:pr is:open archived:false author:@me")
	if err != nil {
		return nil, err
	}
	return searchPRs(items, "yours"), nil
}

func (c CLI) Detail(ctx context.Context, owner, repo string, number int) (model.MergeState, []model.Check, model.Conversation, error) {
	body, err := json.Marshal(map[string]any{"query": detailQuery, "variables": map[string]any{"owner": owner, "repo": repo, "n": number}})
	if err != nil {
		return model.MergeState{}, nil, model.Conversation{}, err
	}
	var raw json.RawMessage
	if err := c.api(ctx, body, &raw, "graphql", "--input", "-"); err != nil {
		return model.MergeState{}, nil, model.Conversation{}, err
	}
	return parseDetail(raw)
}

func (c CLI) Reply(ctx context.Context, _ model.PR, threadID, body string) error {
	return c.graphql(ctx, `mutation($t:ID!,$b:String!){ addPullRequestReviewThreadReply(input:{pullRequestReviewThreadId:$t, body:$b}){ comment{ id } } }`,
		map[string]any{"t": threadID, "b": body}, nil)
}

func (c CLI) Resolve(ctx context.Context, _ model.PR, threadID string, resolved bool) error {
	q := `mutation($t:ID!){ resolveReviewThread(input:{threadId:$t}){ thread{ id } } }`
	if !resolved {
		q = `mutation($t:ID!){ unresolveReviewThread(input:{threadId:$t}){ thread{ id } } }`
	}
	return c.graphql(ctx, q, map[string]any{"t": threadID}, nil)
}

func (c CLI) rest(ctx context.Context, method, path string, in any) error {
	var body []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = b
	}
	args := []string{"-X", method}
	if body != nil {
		args = append(args, "--input", "-")
	}
	return c.api(ctx, body, nil, append(args, path)...)
}

func prPath(pr model.PR, rest string) string {
	return fmt.Sprintf("repos/%s/%s/%s", pr.Owner, pr.Repo, rest)
}

func (c CLI) Comment(ctx context.Context, pr model.PR, body string) error {
	return c.rest(ctx, "POST", prPath(pr, fmt.Sprintf("issues/%d/comments", pr.Number)), map[string]string{"body": body})
}

func (c CLI) Merge(ctx context.Context, pr model.PR, method, sha string) error {
	in := map[string]string{"merge_method": strings.ToLower(method)}
	if sha != "" {
		in["sha"] = sha
	}
	return c.rest(ctx, "PUT", prPath(pr, fmt.Sprintf("pulls/%d/merge", pr.Number)), in)
}

func (c CLI) AutoMerge(ctx context.Context, _ model.PR, nodeID, method string, on bool) error {
	if !on {
		return c.graphql(ctx, `mutation($p:ID!){ disablePullRequestAutoMerge(input:{pullRequestId:$p}){ clientMutationId } }`,
			map[string]any{"p": nodeID}, nil)
	}
	return c.graphql(ctx, `mutation($p:ID!,$m:PullRequestMergeMethod!){ enablePullRequestAutoMerge(input:{pullRequestId:$p, mergeMethod:$m}){ clientMutationId } }`,
		map[string]any{"p": nodeID, "m": strings.ToUpper(method)}, nil)
}

func (c CLI) UpdateBranch(ctx context.Context, pr model.PR) error {
	in := map[string]string{}
	if pr.HeadSHA != "" {
		in["expected_head_sha"] = pr.HeadSHA
	}
	return c.rest(ctx, "PUT", prPath(pr, fmt.Sprintf("pulls/%d/update-branch", pr.Number)), in)
}

func (c CLI) MarkReady(ctx context.Context, _ model.PR, nodeID string) error {
	return c.graphql(ctx, `mutation($p:ID!){ markPullRequestReadyForReview(input:{pullRequestId:$p}){ clientMutationId } }`,
		map[string]any{"p": nodeID}, nil)
}

func (c CLI) RequestReview(ctx context.Context, pr model.PR, logins []string) error {
	if len(logins) == 0 {
		return errors.New("name someone to ask for a review")
	}
	return c.rest(ctx, "POST", prPath(pr, fmt.Sprintf("pulls/%d/requested_reviewers", pr.Number)), map[string][]string{"reviewers": logins})
}

func (c CLI) Rerun(ctx context.Context, pr model.PR, runID int64) error {
	return c.rest(ctx, "POST", prPath(pr, fmt.Sprintf("actions/runs/%d/rerun-failed-jobs", runID)), nil)
}

// LogLines is how much of a failed job's log Docket keeps: the end, where
// the failure is.
const LogLines = 80

func (c CLI) FailedLog(ctx context.Context, pr model.PR, jobID int64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	// The logs endpoint answers with plain text, not JSON.
	cmd := exec.CommandContext(ctx, c.bin(), "api", prPath(pr, fmt.Sprintf("actions/jobs/%d/logs", jobID)))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("could not read the job's log: %s", msg)
	}
	return Tail(stdout.String(), LogLines), nil
}

// Tail returns the last n lines of s.
func Tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
