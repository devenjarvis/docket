package review_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devenjarvis/docket/internal/demo"
	"github.com/devenjarvis/docket/internal/gh"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/review"
	"github.com/devenjarvis/docket/internal/rules"
)

// The test binary doubles as the scripted ACP agent, so the whole loop runs
// over a real pipe: start a process, speak the protocol, write the brief
// through the client's file methods.
func TestMain(m *testing.M) {
	if os.Getenv("DOCKET_TEST_AGENT") == "1" {
		_ = demo.ServeAgent(os.Stdin, os.Stdout)
		return
	}
	os.Exit(m.Run())
}

func start(t *testing.T, setup ...func(*review.Engine, string)) (*review.Engine, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("DOCKET_HOME", home)
	t.Setenv("DOCKET_DEMO_STEP_MS", "1")
	rs, err := rules.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	demo.Seed(home, rs)
	eng := review.New(home, demo.GitHub{Home: home}, demo.Checkout{Home: home}, rs)
	self, _ := os.Executable()
	eng.Agent = func() (string, string) { return fmt.Sprintf("DOCKET_TEST_AGENT=1 %q", self), "test agent" }
	eng.Inbox = demo.GitHub{Home: home}
	for _, f := range setup {
		f(eng, home)
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Cleanups run last-registered first, so the engine stops writing into
	// home before TempDir removes it.
	t.Cleanup(func() { cancel(); eng.Wait() })
	eng.Start(ctx, 2, time.Hour)
	return eng, home
}

func waitReady(t *testing.T, eng *review.Engine, key string) *review.View {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if v, ok := eng.Get(key); ok {
			switch v.Status {
			case "ready":
				return v
			case "failed":
				t.Fatalf("%s failed: %s", key, v.Error)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s was never ready", key)
	return nil
}

func TestAssignedPullRequestsAreReadWithoutBeingAsked(t *testing.T) {
	eng, _ := start(t)
	const key = "acme/payments-api#4127"
	v := waitReady(t, eng, key)

	if got := len(v.Brief.Decisions); got != 3 {
		t.Fatalf("decisions = %d, want 3", got)
	}
	// Docket counts the lines itself rather than trusting the agent.
	if v.Brief.Sections[0].Lines == 0 {
		t.Error("section line counts were not filled in from the diff")
	}
	if len(v.Hot["d1"]) == 0 || len(v.Sections["s1"]) != 2 {
		t.Errorf("diffs were not cut to the brief: hot=%d s1=%d", len(v.Hot["d1"]), len(v.Sections["s1"]))
	}
	hot := 0
	for _, h := range v.Hot["d1"][0].Hunks {
		for _, l := range h.Lines {
			if l.Hot {
				hot++
			}
		}
	}
	if hot != 13 {
		t.Errorf("hot lines for d1 = %d, want the 13 added lines of the retry loop", hot)
	}
	// Other reviewers' threads reach the agent.
	b, err := os.ReadFile(filepath.Join(filepath.Dir(v.OutDir), "conversation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var conv model.Conversation
	if err := json.Unmarshal(b, &conv); err != nil || len(conv.Threads) != 1 || conv.Threads[0].Comments[0].Author != "lee-k" {
		t.Errorf("conversation.json = %s (%v)", b, err)
	}
	if st := eng.List(); len(st.Reviews) != 3 || st.Reviews[0].Key != key {
		t.Errorf("queue should lead with the PR that has the most at stake, got %+v", st.Reviews)
	}
}

func TestApproveStaysLockedUntilEveryDecisionIsSettled(t *testing.T) {
	eng, home := start(t)
	const key = "acme/payments-api#4127"
	waitReady(t, eng, key)
	ctx := context.Background()

	if _, err := eng.Post(ctx, key, review.PostInput{Event: "APPROVE"}); err == nil {
		t.Fatal("approved with three unanswered decisions")
	}
	for d, opt := range map[string]string{"d1": "block", "d2": "accept", "d3": "accept"} {
		if err := eng.SetAnswer(key, d, model.Answer{Option: opt}); err != nil {
			t.Fatal(err)
		}
	}
	draft, err := eng.Compose(key, true)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Event != "REQUEST_CHANGES" || len(draft.Locks) != 1 {
		t.Fatalf("event=%s locks=%v, want REQUEST_CHANGES with one lock", draft.Event, draft.Locks)
	}
	if _, err := eng.Post(ctx, key, review.PostInput{Event: "APPROVE"}); err == nil {
		t.Fatal("approved with a blocking decision")
	}
	if draft.Body != "" {
		t.Errorf("the summary should start empty, for the reviewer's own words; got %q", draft.Body)
	}
	if !draft.IncludeReceipt || !strings.Contains(draft.ReceiptMarkdown, "How this was reviewed") {
		t.Error("the attention receipt is missing from the draft")
	}
	if l := draft.Locks[0]; l.Decision != "d1" {
		t.Errorf("the blocking lock should lead to d1, got %+v", l)
	}

	if _, err := eng.Post(ctx, key, review.PostInput{Event: draft.Event, Body: "Looks close.", Comments: draft.Comments, Receipt: true}); err != nil {
		t.Fatal(err)
	}
	var posted gh.ReviewInput
	b, err := os.ReadFile(filepath.Join(home, "posted", "payments-api-4127.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &posted); err != nil {
		t.Fatal(err)
	}
	if posted.Event != "REQUEST_CHANGES" || len(posted.Comments) != 2 {
		t.Fatalf("posted %s with %d inline comments, want REQUEST_CHANGES with 2", posted.Event, len(posted.Comments))
	}
	if c := posted.Comments[0]; c.Path != "src/delivery/deliver.ts" || c.Line != 20 || !strings.Contains(c.Body, "Failing probe") {
		t.Errorf("blocking comment should sit on the anchor and carry its probe: %+v", c)
	}
	if !strings.Contains(posted.Body, "Looks close.") || !strings.HasSuffix(posted.Body, "</details>") {
		t.Errorf("the body should carry the summary and end with the receipt, got %q", posted.Body)
	}
}

func TestAnsweringTeachesTheRulebook(t *testing.T) {
	eng, _ := start(t)
	const key = "acme/payments-api#4127"
	waitReady(t, eng, key)
	before, _ := eng.Rules.Get("R07")

	if err := eng.SetAnswer(key, "d1", model.Answer{Option: "block"}); err != nil {
		t.Fatal(err)
	}
	after, _ := eng.Rules.Get("R07")
	if after.Stats.Agreed != before.Stats.Agreed+1 {
		t.Errorf("acting on a rule's finding should count as agreeing: %d → %d", before.Stats.Agreed, after.Stats.Agreed)
	}
	// Changing the verdict moves the count rather than adding to it.
	if err := eng.SetAnswer(key, "d1", model.Answer{Option: "accept", Feedback: "not-here"}); err != nil {
		t.Fatal(err)
	}
	moved, _ := eng.Rules.Get("R07")
	if moved.Stats.Fired != after.Stats.Fired || moved.Stats.Overrode != before.Stats.Overrode+1 || moved.Stats.Agreed != before.Stats.Agreed {
		t.Errorf("stats after changing the verdict: %+v", moved.Stats)
	}
	if len(moved.Except) != 1 || moved.Except[0] != "acme/payments-api" {
		t.Errorf("\"not in this repo\" should exempt the repository: %v", moved.Except)
	}
	if rs, _ := eng.Rules.ForRepo("acme/payments-api"); containsRule(rs, "R07") {
		t.Error("an exempted rule was still handed to the agent for that repository")
	}
}

func containsRule(rs []model.Rule, id string) bool {
	for _, r := range rs {
		if r.ID == id {
			return true
		}
	}
	return false
}

// closingGitHub is the demo GitHub, except that the PRs in merged have left
// the search results and read back as merged.
type closingGitHub struct {
	demo.GitHub
	mu     sync.Mutex
	merged map[string]bool
	hidden map[string]bool
	broken map[string]bool
}

func (g *closingGitHub) Assigned(ctx context.Context) ([]model.PR, error) {
	all, err := g.GitHub.Assigned(ctx)
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []model.PR
	for _, p := range all {
		if !g.merged[p.Key()] && !g.hidden[p.Key()] {
			out = append(out, p)
		}
	}
	return out, err
}

func (g *closingGitHub) PR(ctx context.Context, owner, repo string, n int) (model.PR, error) {
	p, err := g.GitHub.PR(ctx, owner, repo, n)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.broken[model.Key(owner, repo, n)] {
		return model.PR{}, errors.New("GitHub is down")
	}
	if err == nil && g.merged[p.Key()] {
		p.State = "merged"
	}
	return p, err
}

func TestMergedPullRequestsLeaveTheQueue(t *testing.T) {
	const merged, open, unknown = "acme/ledger#311", "acme/web-app#2214", "acme/payments-api#4127"
	fake := &closingGitHub{merged: map[string]bool{}, broken: map[string]bool{},
		hidden: map[string]bool{merged: true, open: true, unknown: true}}
	eng, home := start(t, func(e *review.Engine, home string) {
		fake.GitHub = demo.GitHub{Home: home}
		e.GH = fake
	})
	ctx := context.Background()
	for _, ref := range []string{merged, open, unknown} {
		if _, err := eng.Add(ctx, ref); err != nil {
			t.Fatal(err)
		}
	}
	waitReady(t, eng, merged)
	waitReady(t, eng, open)
	waitReady(t, eng, unknown)

	fake.mu.Lock()
	fake.merged[merged] = true
	fake.broken[unknown] = true
	fake.mu.Unlock()
	if err := eng.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	if _, ok := eng.Get(merged); ok {
		t.Errorf("%s is merged but is still in the queue", merged)
	}
	if _, err := os.Stat(filepath.Join(home, "state", "acme__ledger__311.json")); !os.IsNotExist(err) {
		t.Errorf("the state file of %s is still there (%v)", merged, err)
	}
	if _, ok := eng.Get(open); !ok {
		t.Errorf("%s is still open but left the queue", open)
	}
	if _, ok := eng.Get(unknown); !ok {
		t.Errorf("%s left the queue although GitHub could not say whether it is open", unknown)
	}
}
