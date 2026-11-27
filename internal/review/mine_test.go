package review_test

import (
	"context"
	"sync"
	"testing"

	"github.com/devenjarvis/docket/internal/demo"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/review"
)

type notes struct {
	mu   sync.Mutex
	list []string
}

// add keeps only notifications about the user's own pull requests; reads
// of review requests notify too.
func (n *notes) add(title, body, key string) {
	switch key {
	case demo.ReadyKey, demo.BlockedKey, demo.ConflictKey:
	default:
		return
	}
	n.mu.Lock()
	n.list = append(n.list, key+": "+title)
	n.mu.Unlock()
}

func (n *notes) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.list)
}

func TestOwnPullRequestsAreWatchedAndActedOn(t *testing.T) {
	var n notes
	eng, home := start(t, func(e *review.Engine, _ string) { e.Notify = n.add })
	ctx := context.Background()

	if err := eng.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	lanes := map[string]string{}
	for _, m := range eng.Mine() {
		lanes[m.PR.Key()] = m.Lane
	}
	want := map[string]string{demo.ReadyKey: model.LaneReady, demo.BlockedKey: model.LaneNeedsYou, demo.ConflictKey: model.LaneNeedsYou}
	for k, l := range want {
		if lanes[k] != l {
			t.Errorf("%s lane = %q, want %q (all: %v)", k, lanes[k], l, lanes)
		}
	}
	if c := n.count(); c != 0 {
		t.Errorf("the first look notified %d times, want none", c)
	}

	m, _ := eng.MineOne(demo.BlockedKey)
	thread := m.Conversation.Threads[0].ID
	if err := eng.Act(ctx, demo.BlockedKey, review.Action{Kind: "reply", Thread: thread, Body: "Renamed."}); err != nil {
		t.Fatal(err)
	}
	m, _ = eng.MineOne(demo.BlockedKey)
	if last := m.Conversation.Threads[0].Last(); last.Body != "Renamed." {
		t.Errorf("the reply is not in the conversation: %+v", last)
	}
	if err := eng.Act(ctx, demo.BlockedKey, review.Action{Kind: "merge", Method: "SQUASH"}); err == nil {
		t.Error("merged with failing checks")
	}
	if err := eng.Act(ctx, demo.BlockedKey, review.Action{Kind: "explode"}); err == nil {
		t.Error("an unknown action was accepted")
	}

	// A comment from a reviewer on the next look is news; the user's own
	// actions are not.
	before := n.count()
	if err := (demo.GitHub{Home: home}).Comment(ctx, model.PR{Owner: "acme", Repo: "web-app", Number: 2230}, "x"); err != nil {
		t.Fatal(err)
	}
	if err := eng.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if n.count() != before {
		t.Errorf("the user's own comment raised a notification")
	}

	if err := eng.Act(ctx, demo.ReadyKey, review.Action{Kind: "merge", Method: "SQUASH"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := eng.MineOne(demo.ReadyKey); ok {
		t.Error("a merged pull request is still listed")
	}

	conv, err := eng.Conversation(ctx, "acme/payments-api#4127")
	if err != nil || len(conv.Threads) != 1 {
		t.Errorf("review conversation = %+v (%v)", conv, err)
	}
}
