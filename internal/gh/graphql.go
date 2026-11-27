package gh

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/model"
)

// graphql runs one GraphQL request through `gh api graphql` and decodes its
// data into out.
func (c CLI) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	var raw json.RawMessage
	if err := c.api(ctx, body, &raw, "graphql", "--input", "-"); err != nil {
		return err
	}
	return decodeGraphQL(raw, out)
}

func decodeGraphQL(raw []byte, out any) error {
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	if len(env.Errors) > 0 {
		var msgs []string
		for _, e := range env.Errors {
			msgs = append(msgs, e.Message)
		}
		return errors.New("GitHub: " + strings.Join(msgs, "; "))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

// Pages of 100 cover ordinary pull requests; a longer conversation shows its
// latest comments and first threads.
const detailQuery = `query($owner:String!,$repo:String!,$n:Int!){
  repository(owner:$owner,name:$repo){
    mergeCommitAllowed squashMergeAllowed rebaseMergeAllowed autoMergeAllowed
    pullRequest(number:$n){
      id number isDraft mergeable mergeStateStatus reviewDecision maintainerCanModify
      autoMergeRequest{ mergeMethod }
      headRepository{ nameWithOwner }
      reviewRequests(first:20){ nodes{ requestedReviewer{ ... on User{login} ... on Team{slug} } } }
      commits(last:1){ nodes{ commit{ statusCheckRollup{ contexts(first:100){ nodes{
        __typename
        ... on CheckRun{ id name status conclusion detailsUrl startedAt completedAt databaseId isRequired(pullRequestNumber:$n)
          checkSuite{ workflowRun{ databaseId workflow{ name } } } }
        ... on StatusContext{ id context state targetUrl createdAt isRequired(pullRequestNumber:$n) }
      } } } } } }
      reviews(last:50){ nodes{ id author{login} state body url submittedAt } }
      reviewThreads(first:100){ nodes{ id path line isResolved isOutdated
        comments(first:50){ nodes{ id author{login} body url createdAt diffHunk } } } }
      comments(last:100){ nodes{ id author{login} body url createdAt } }
    }
  }
}`

type gqlAuthor *struct {
	Login string `json:"login"`
}

func login(a gqlAuthor) string {
	if a == nil || a.Login == "" {
		return "ghost" // GitHub's name for a deleted account
	}
	return a.Login
}

type gqlComment struct {
	ID        string    `json:"id"`
	Author    gqlAuthor `json:"author"`
	Body      string    `json:"body"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
	DiffHunk  string    `json:"diffHunk"`
}

func (g gqlComment) model() model.Comment {
	return model.Comment{ID: g.ID, Author: login(g.Author), Body: g.Body, URL: g.URL, At: g.CreatedAt}
}

type gqlDetail struct {
	Repository struct {
		MergeCommitAllowed bool `json:"mergeCommitAllowed"`
		SquashMergeAllowed bool `json:"squashMergeAllowed"`
		RebaseMergeAllowed bool `json:"rebaseMergeAllowed"`
		AutoMergeAllowed   bool `json:"autoMergeAllowed"`
		PullRequest        *struct {
			ID                  string  `json:"id"`
			Mergeable           string  `json:"mergeable"`
			MergeStateStatus    string  `json:"mergeStateStatus"`
			ReviewDecision      *string `json:"reviewDecision"`
			MaintainerCanModify bool    `json:"maintainerCanModify"`
			AutoMergeRequest    *struct {
				MergeMethod string `json:"mergeMethod"`
			} `json:"autoMergeRequest"`
			HeadRepository *struct {
				NameWithOwner string `json:"nameWithOwner"`
			} `json:"headRepository"`
			ReviewRequests struct {
				Nodes []struct {
					RequestedReviewer *struct {
						Login string `json:"login"`
						Slug  string `json:"slug"`
					} `json:"requestedReviewer"`
				} `json:"nodes"`
			} `json:"reviewRequests"`
			Commits struct {
				Nodes []struct {
					Commit struct {
						StatusCheckRollup *struct {
							Contexts struct {
								Nodes []struct {
									Typename    string    `json:"__typename"`
									ID          string    `json:"id"`
									Name        string    `json:"name"`
									Status      string    `json:"status"`
									Conclusion  string    `json:"conclusion"`
									DetailsURL  string    `json:"detailsUrl"`
									StartedAt   time.Time `json:"startedAt"`
									CompletedAt time.Time `json:"completedAt"`
									DatabaseID  int64     `json:"databaseId"`
									IsRequired  bool      `json:"isRequired"`
									CheckSuite  *struct {
										WorkflowRun *struct {
											DatabaseID int64 `json:"databaseId"`
											Workflow   struct {
												Name string `json:"name"`
											} `json:"workflow"`
										} `json:"workflowRun"`
									} `json:"checkSuite"`
									Context   string    `json:"context"`
									State     string    `json:"state"`
									TargetURL string    `json:"targetUrl"`
									CreatedAt time.Time `json:"createdAt"`
								} `json:"nodes"`
							} `json:"contexts"`
						} `json:"statusCheckRollup"`
					} `json:"commit"`
				} `json:"nodes"`
			} `json:"commits"`
			Reviews struct {
				Nodes []struct {
					ID          string    `json:"id"`
					Author      gqlAuthor `json:"author"`
					State       string    `json:"state"`
					Body        string    `json:"body"`
					URL         string    `json:"url"`
					SubmittedAt time.Time `json:"submittedAt"`
				} `json:"nodes"`
			} `json:"reviews"`
			ReviewThreads struct {
				Nodes []struct {
					ID         string `json:"id"`
					Path       string `json:"path"`
					Line       int    `json:"line"`
					IsResolved bool   `json:"isResolved"`
					IsOutdated bool   `json:"isOutdated"`
					Comments   struct {
						Nodes []gqlComment `json:"nodes"`
					} `json:"comments"`
				} `json:"nodes"`
			} `json:"reviewThreads"`
			Comments struct {
				Nodes []gqlComment `json:"nodes"`
			} `json:"comments"`
		} `json:"pullRequest"`
	} `json:"repository"`
}

// parseDetail reads a detailQuery response.
func parseDetail(raw []byte) (model.MergeState, []model.Check, model.Conversation, error) {
	var d gqlDetail
	var st model.MergeState
	var conv model.Conversation
	if err := decodeGraphQL(raw, &d); err != nil {
		return st, nil, conv, err
	}
	repo := d.Repository
	p := repo.PullRequest
	if p == nil {
		return st, nil, conv, errors.New("GitHub returned no such pull request")
	}
	st = model.MergeState{
		NodeID: p.ID, Mergeable: p.Mergeable, MergeStateStatus: p.MergeStateStatus,
		MaintainerCanModify: p.MaintainerCanModify, AutoMergeAllowed: repo.AutoMergeAllowed,
	}
	if p.ReviewDecision != nil {
		st.ReviewDecision = *p.ReviewDecision
	}
	if p.AutoMergeRequest != nil {
		st.AutoMerge = p.AutoMergeRequest.MergeMethod
	}
	if p.HeadRepository != nil {
		st.HeadRepo = p.HeadRepository.NameWithOwner
	}
	for _, m := range []struct {
		ok   bool
		name string
	}{{repo.MergeCommitAllowed, "MERGE"}, {repo.SquashMergeAllowed, "SQUASH"}, {repo.RebaseMergeAllowed, "REBASE"}} {
		if m.ok {
			st.Methods = append(st.Methods, m.name)
		}
	}
	for _, n := range p.ReviewRequests.Nodes {
		if r := n.RequestedReviewer; r != nil {
			if r.Login != "" {
				st.RequestedReviewers = append(st.RequestedReviewers, r.Login)
			} else if r.Slug != "" {
				st.RequestedReviewers = append(st.RequestedReviewers, r.Slug)
			}
		}
	}

	var checks []model.Check
	for _, c := range p.Commits.Nodes {
		roll := c.Commit.StatusCheckRollup
		if roll == nil {
			continue
		}
		for _, n := range roll.Contexts.Nodes {
			switch n.Typename {
			case "CheckRun":
				ch := model.Check{ID: n.ID, Name: n.Name, Status: n.Status, Conclusion: n.Conclusion, Required: n.IsRequired,
					URL: n.DetailsURL, StartedAt: n.StartedAt, CompletedAt: n.CompletedAt}
				if cs := n.CheckSuite; cs != nil && cs.WorkflowRun != nil {
					ch.RunID, ch.JobID, ch.Workflow = cs.WorkflowRun.DatabaseID, n.DatabaseID, cs.WorkflowRun.Workflow.Name
				}
				checks = append(checks, ch)
			case "StatusContext":
				ch := model.Check{ID: n.ID, Name: n.Context, Required: n.IsRequired, URL: n.TargetURL, StartedAt: n.CreatedAt}
				switch n.State {
				case "PENDING", "EXPECTED":
					ch.Status = "PENDING"
				default:
					ch.Status, ch.Conclusion = "COMPLETED", n.State
				}
				checks = append(checks, ch)
			}
		}
	}

	for _, r := range p.Reviews.Nodes {
		conv.Reviews = append(conv.Reviews, model.ReviewEvent{ID: r.ID, Author: login(r.Author), State: r.State, Body: r.Body, URL: r.URL, At: r.SubmittedAt})
	}
	for _, t := range p.ReviewThreads.Nodes {
		th := model.Thread{ID: t.ID, Path: t.Path, Line: t.Line, Resolved: t.IsResolved, Outdated: t.IsOutdated}
		for i, c := range t.Comments.Nodes {
			if i == 0 {
				th.DiffHunk = c.DiffHunk
			}
			th.Comments = append(th.Comments, c.model())
		}
		conv.Threads = append(conv.Threads, th)
	}
	for _, c := range p.Comments.Nodes {
		conv.Comments = append(conv.Comments, c.model())
	}
	return st, checks, conv, nil
}
