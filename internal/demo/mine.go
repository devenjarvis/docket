package demo

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/gh"
	"github.com/devenjarvis/docket/internal/model"
)

// The demo's own pull requests: one ready, one blocked by CI and a reviewer,
// one in conflict.
const (
	ReadyKey    = "acme/payments-api#4133"
	BlockedKey  = "acme/ledger#318"
	ConflictKey = "acme/web-app#2230"
	viewer      = "devenjarvis"
)

var _ gh.Inbox = GitHub{}

type ownPR struct {
	pr     model.PR
	st     model.MergeState
	checks []model.Check
	conv   model.Conversation
	// rerunAt is when the checks started again; they finish a few seconds
	// later, failing until the nil map is fixed.
	rerunAt time.Time
	fixed   bool
}

// inbox is the demo's GitHub state. Actions change it, so the next Detail
// shows what the user did, as GitHub would.
type inbox struct {
	mu     sync.Mutex
	own    []*ownPR
	review map[string]*model.Conversation
	seq    int
}

var (
	inboxesMu sync.Mutex
	inboxes   = map[string]*inbox{}
)

// state returns the inbox for one demo home, so separate runs and tests do
// not see each other's actions.
func (g GitHub) state() *inbox {
	inboxesMu.Lock()
	defer inboxesMu.Unlock()
	if in := inboxes[g.Home]; in != nil {
		return in
	}
	in := seedInbox()
	inboxes[g.Home] = in
	return in
}

func at(d time.Duration) time.Time { return now.Add(-d) }

func seedInbox() *inbox {
	all := []string{"MERGE", "SQUASH", "REBASE"}
	passed := func(name string, d time.Duration) model.Check {
		return model.Check{ID: "CR_" + name, Name: name, Workflow: "CI", Status: "COMPLETED", Conclusion: "SUCCESS", Required: true,
			StartedAt: at(d + 3*time.Minute), CompletedAt: at(d)}
	}
	in := &inbox{review: map[string]*model.Conversation{}}
	in.own = []*ownPR{
		{
			pr: model.PR{Owner: "acme", Repo: "payments-api", Number: 4133, Title: "Add request ids to webhook delivery logs", Author: viewer,
				Body:    "Every delivery log line now carries the request id, so a failed delivery can be traced end to end.",
				HeadSHA: "e1a2b3c4d5f60718", BaseSHA: "9f2c1d7a40b1", BaseRef: "main", HeadRef: "deven/delivery-request-ids",
				Additions: 22, Deletions: 6, Files: 3, UpdatedAt: at(2 * time.Hour)},
			st:     model.MergeState{NodeID: "PR_4133", Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN", ReviewDecision: "APPROVED", Methods: []string{"SQUASH", "REBASE"}, AutoMergeAllowed: true, HeadRepo: "acme/payments-api"},
			checks: []model.Check{passed("test", 2*time.Hour), passed("lint", 2*time.Hour)},
			conv: model.Conversation{Reviews: []model.ReviewEvent{
				{ID: "R_4133_1", Author: "maya-r", State: "APPROVED", Body: "Clean. Thanks for threading it through the retry path too.", At: at(90 * time.Minute)},
			}},
		},
		{
			pr: model.PR{Owner: "acme", Repo: "ledger", Number: 318, Title: "Cache currency metadata at startup", Author: viewer,
				Body:    "Currency metadata was read from the database on every settlement. It is now loaded once at startup and refreshed hourly.",
				HeadSHA: "7c0ffee1234abcd5", BaseSHA: "c41a9e0b77d2", BaseRef: "main", HeadRef: "deven/currency-cache",
				Additions: 74, Deletions: 12, Files: 4, UpdatedAt: at(25 * time.Minute)},
			st: model.MergeState{NodeID: "PR_318", Mergeable: "MERGEABLE", MergeStateStatus: "BLOCKED", ReviewDecision: "CHANGES_REQUESTED", Methods: all, HeadRepo: "acme/ledger"},
			checks: []model.Check{
				{ID: "CR_test", Name: "test", Workflow: "CI", Status: "COMPLETED", Conclusion: "FAILURE", Required: true, StartedAt: at(30 * time.Minute), CompletedAt: at(27 * time.Minute), RunID: 7001, JobID: 9001,
					URL: "https://github.com/acme/ledger/actions/runs/7001/job/9001"},
				passed("lint", 27*time.Minute),
			},
			conv: model.Conversation{
				Reviews: []model.ReviewEvent{{ID: "R_318_1", Author: "sam-t", State: "CHANGES_REQUESTED", Body: "Two things before this goes in.", At: at(20 * time.Minute)}},
				Threads: []model.Thread{
					{ID: "T_318_1", Path: "src/currency/cache.go", Line: 14,
						DiffHunk: "@@ -10,6 +10,12 @@ package currency\n+// curMeta holds the metadata loaded at startup.\n+var curMeta map[string]Meta",
						Comments: []model.Comment{{ID: "C_318_1", Author: "sam-t", Body: "Name this `currencyMeta`: everything else in the package spells it out.", At: at(20 * time.Minute)}}},
					{ID: "T_318_2", Path: "src/currency/cache.go", Line: 31,
						DiffHunk: "@@ -28,4 +28,10 @@ func Load(db *sql.DB) error {\n+\tticker := time.NewTicker(time.Hour)",
						Comments: []model.Comment{{ID: "C_318_2", Author: "sam-t", Body: "Can we make the refresh interval configurable? Ops will want to tune it.", At: at(19 * time.Minute)}}},
				},
			},
		},
		{
			pr: model.PR{Owner: "acme", Repo: "web-app", Number: 2230, Title: "Move date helpers into lib/dates", Author: viewer,
				Body:    "Pure move: the date helpers scattered across components now live in `lib/dates`.",
				HeadSHA: "d47e5a6b7c8d9e0f", BaseSHA: "aa91", BaseRef: "main", HeadRef: "deven/lib-dates",
				Additions: 140, Deletions: 131, Files: 11, UpdatedAt: at(5 * time.Hour)},
			st:     model.MergeState{NodeID: "PR_2230", Mergeable: "CONFLICTING", MergeStateStatus: "DIRTY", ReviewDecision: "REVIEW_REQUIRED", Methods: all, HeadRepo: "acme/web-app", RequestedReviewers: []string{"priya-n"}},
			checks: []model.Check{passed("test", 5*time.Hour)},
		},
	}
	in.review["acme/payments-api#4127"] = &model.Conversation{
		Reviews: []model.ReviewEvent{{ID: "R_4127_1", Author: "lee-k", State: "COMMENTED", Body: "Only looked at the migration.", At: at(30 * time.Minute)}},
		Threads: []model.Thread{{ID: "T_4127_1", Path: "migrations/0042_attempts.sql", Line: 2,
			Comments: []model.Comment{
				{ID: "C_4127_1", Author: "lee-k", Body: "This adds a NOT NULL column with no default. What is the deploy order?", At: at(30 * time.Minute)},
				{ID: "C_4127_2", Author: "maya-r", Body: "Migration first, then code. I will put it in the description.", At: at(25 * time.Minute)},
			}}},
	}
	return in
}

func (in *inbox) find(owner, repo string, n int) *ownPR {
	k := model.Key(owner, repo, n)
	for _, o := range in.own {
		if o.pr.Key() == k {
			return o
		}
	}
	return nil
}

// conv returns the conversation for any demo pull request, own or not.
func (in *inbox) conv(pr model.PR) (*model.Conversation, *ownPR) {
	if o := in.find(pr.Owner, pr.Repo, pr.Number); o != nil {
		return &o.conv, o
	}
	c := in.review[pr.Key()]
	if c == nil {
		c = &model.Conversation{}
		in.review[pr.Key()] = c
	}
	return c, nil
}

func (in *inbox) id(prefix string) string {
	in.seq++
	return fmt.Sprintf("%s_demo_%d", prefix, in.seq)
}

func (g GitHub) Mine(context.Context) ([]model.PR, error) {
	in := g.state()
	in.mu.Lock()
	defer in.mu.Unlock()
	var out []model.PR
	for _, o := range in.own {
		p := o.pr
		p.Body, p.Why = "", []string{"yours"}
		out = append(out, p)
	}
	return out, nil
}

func (g GitHub) Detail(_ context.Context, owner, repo string, n int) (model.MergeState, []model.Check, model.Conversation, error) {
	in := g.state()
	in.mu.Lock()
	defer in.mu.Unlock()
	if o := in.find(owner, repo, n); o != nil {
		if !o.rerunAt.IsZero() && time.Since(o.rerunAt) > 4*time.Second {
			for i := range o.checks {
				if !o.checks[i].Pending() {
					continue
				}
				o.checks[i].Status, o.checks[i].Conclusion, o.checks[i].CompletedAt = "COMPLETED", "SUCCESS", time.Now()
				if o.checks[i].RunID != 0 && !o.fixed {
					o.checks[i].Conclusion = "FAILURE"
				}
			}
			o.rerunAt = time.Time{}
			o.pr.UpdatedAt = time.Now()
		}
		return o.st, slices.Clone(o.checks), cloneConv(o.conv), nil
	}
	for _, p := range prs {
		if p.Owner == owner && p.Repo == repo && p.Number == n {
			c, _ := in.conv(p)
			return model.MergeState{Mergeable: "MERGEABLE", MergeStateStatus: "BLOCKED", ReviewDecision: "REVIEW_REQUIRED", Methods: []string{"SQUASH"}, HeadRepo: owner + "/" + repo},
				nil, cloneConv(*c), nil
		}
	}
	return model.MergeState{}, nil, model.Conversation{}, fmt.Errorf("the demo has no pull request %s", model.Key(owner, repo, n))
}

func cloneConv(c model.Conversation) model.Conversation {
	out := model.Conversation{Reviews: slices.Clone(c.Reviews), Comments: slices.Clone(c.Comments)}
	for _, t := range c.Threads {
		t.Comments = slices.Clone(t.Comments)
		out.Threads = append(out.Threads, t)
	}
	return out
}

// touch marks a change, so the engine's next look fetches the detail again.
func (o *ownPR) touch() {
	if o != nil {
		o.pr.UpdatedAt = time.Now()
	}
}

func (g GitHub) Reply(_ context.Context, pr model.PR, threadID, body string) (string, error) {
	in := g.state()
	in.mu.Lock()
	defer in.mu.Unlock()
	c, o := in.conv(pr)
	for i := range c.Threads {
		if c.Threads[i].ID == threadID {
			id := in.id("C")
			c.Threads[i].Comments = append(c.Threads[i].Comments, model.Comment{ID: id, Author: viewer, Body: body, At: time.Now()})
			o.touch()
			return id, nil
		}
	}
	return "", errors.New("no such thread")
}

func (g GitHub) DeleteComment(_ context.Context, pr model.PR, commentID string) error {
	in := g.state()
	in.mu.Lock()
	defer in.mu.Unlock()
	c, o := in.conv(pr)
	for i := range c.Threads {
		for j, cm := range c.Threads[i].Comments {
			if cm.ID == commentID {
				if cm.Author != viewer {
					return errors.New("you can only delete your own comments")
				}
				c.Threads[i].Comments = append(c.Threads[i].Comments[:j], c.Threads[i].Comments[j+1:]...)
				o.touch()
				return nil
			}
		}
	}
	return errors.New("no such comment")
}

func (g GitHub) Resolve(_ context.Context, pr model.PR, threadID string, resolved bool) error {
	in := g.state()
	in.mu.Lock()
	defer in.mu.Unlock()
	c, o := in.conv(pr)
	for i := range c.Threads {
		if c.Threads[i].ID == threadID {
			c.Threads[i].Resolved = resolved
			o.touch()
			return nil
		}
	}
	return errors.New("no such thread")
}

func (g GitHub) Comment(_ context.Context, pr model.PR, body string) error {
	in := g.state()
	in.mu.Lock()
	defer in.mu.Unlock()
	c, o := in.conv(pr)
	c.Comments = append(c.Comments, model.Comment{ID: in.id("IC"), Author: viewer, Body: body, At: time.Now()})
	o.touch()
	return nil
}

func (g GitHub) own(pr model.PR) (*inbox, *ownPR, error) {
	in := g.state()
	in.mu.Lock()
	o := in.find(pr.Owner, pr.Repo, pr.Number)
	if o == nil {
		in.mu.Unlock()
		return nil, nil, errors.New("only your own pull requests can do that in the demo")
	}
	return in, o, nil
}

func (g GitHub) Merge(_ context.Context, pr model.PR, method, sha string) error {
	in, o, err := g.own(pr)
	if err != nil {
		return err
	}
	defer in.mu.Unlock()
	if sha != "" && sha != o.pr.HeadSHA {
		return errors.New("Head branch was modified. Review and try the merge again.")
	}
	if !slices.Contains(o.st.Methods, strings.ToUpper(method)) {
		return fmt.Errorf("%s merges are not allowed in this repository", strings.ToLower(method))
	}
	if bs := model.Blockers(viewer, o.pr.Draft, o.st, o.checks, o.conv); len(bs) > 0 {
		return errors.New("Pull request is not mergeable: " + bs[0].Text)
	}
	in.own = slices.DeleteFunc(in.own, func(x *ownPR) bool { return x == o })
	return config.WriteJSON(filepath.Join(g.Home, "posted", fmt.Sprintf("merged-%s-%d.json", pr.Repo, pr.Number)), map[string]string{"method": method, "sha": sha})
}

func (g GitHub) AutoMerge(_ context.Context, pr model.PR, _, method string, on bool) error {
	in, o, err := g.own(pr)
	if err != nil {
		return err
	}
	defer in.mu.Unlock()
	if !o.st.AutoMergeAllowed {
		return errors.New("auto-merge is not allowed for this repository")
	}
	o.st.AutoMerge = ""
	if on {
		o.st.AutoMerge = strings.ToUpper(method)
	}
	o.touch()
	return nil
}

func (g GitHub) UpdateBranch(_ context.Context, pr model.PR) error {
	in, o, err := g.own(pr)
	if err != nil {
		return err
	}
	defer in.mu.Unlock()
	if o.st.Mergeable == "CONFLICTING" {
		return errors.New("merge conflict between base and head")
	}
	if o.st.MergeStateStatus == "BEHIND" {
		o.st.MergeStateStatus = "CLEAN"
	}
	o.touch()
	return nil
}

func (g GitHub) MarkReady(_ context.Context, pr model.PR, _ string) error {
	in, o, err := g.own(pr)
	if err != nil {
		return err
	}
	defer in.mu.Unlock()
	o.pr.Draft = false
	o.touch()
	return nil
}

func (g GitHub) RequestReview(_ context.Context, pr model.PR, logins []string) error {
	in, o, err := g.own(pr)
	if err != nil {
		return err
	}
	defer in.mu.Unlock()
	for _, l := range logins {
		if !slices.Contains(o.st.RequestedReviewers, l) {
			o.st.RequestedReviewers = append(o.st.RequestedReviewers, l)
		}
	}
	o.touch()
	return nil
}

func (g GitHub) Rerun(_ context.Context, pr model.PR, runID int64) error {
	in, o, err := g.own(pr)
	if err != nil {
		return err
	}
	defer in.mu.Unlock()
	n := 0
	for i := range o.checks {
		if o.checks[i].RunID == runID && o.checks[i].Failed() {
			o.checks[i].Status, o.checks[i].Conclusion, o.checks[i].StartedAt, o.checks[i].CompletedAt = "IN_PROGRESS", "", time.Now(), time.Time{}
			n++
		}
	}
	if n == 0 {
		return errors.New("that run has no failed jobs")
	}
	o.rerunAt = time.Now()
	o.touch()
	return nil
}

const failedLog = `##[group]Run go test ./...
ok   github.com/acme/ledger/src/outbox      0.41s
--- FAIL: TestSettleUsesCachedMeta (0.00s)
panic: assignment to entry in nil map [recovered]
	panic: assignment to entry in nil map

goroutine 7 [running]:
github.com/acme/ledger/src/currency.Refresh(...)
	/home/runner/work/ledger/src/currency/cache.go:22 +0x4c
github.com/acme/ledger/src/currency.TestSettleUsesCachedMeta(0xc000102340)
	/home/runner/work/ledger/src/currency/cache_test.go:17 +0x2d
FAIL github.com/acme/ledger/src/currency    0.02s
FAIL
##[error]Process completed with exit code 1.`

func (g GitHub) FailedLog(_ context.Context, pr model.PR, jobID int64) (string, error) {
	in, o, err := g.own(pr)
	if err != nil {
		return "", err
	}
	defer in.mu.Unlock()
	for _, c := range o.checks {
		if c.JobID == jobID {
			return failedLog, nil
		}
	}
	return "", errors.New("no such job")
}
