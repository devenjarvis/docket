package review_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devenjarvis/docket/internal/demo"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/review"
)

func startTend(t *testing.T, mode string) (*review.Engine, string) {
	t.Helper()
	return start(t, func(e *review.Engine, home string) {
		e.Branches = demo.Checkout{Home: home}
		e.TendMode = func() string { return mode }
	})
}

// waitTend waits until the tend run on key settles and returns the PR.
func waitTend(t *testing.T, eng *review.Engine, key string) model.Mine {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if m, ok := eng.MineOne(key); ok && m.Tend != nil {
			switch m.Tend.Status {
			case "ready":
				return m
			case "failed":
				t.Fatalf("tend failed: %s", m.Tend.Error)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s was never tended", key)
	return model.Mine{}
}

func action(t *testing.T, m model.Mine, id string) model.TendAction {
	t.Helper()
	for _, a := range m.Tend.Actions {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("no action %s in %+v", id, m.Tend.Actions)
	return model.TendAction{}
}

func TestTheAgentActsOnItsOwnWhenItIsSafeAndAsksOtherwise(t *testing.T) {
	eng, _ := startTend(t, "auto")
	ctx := context.Background()
	if err := eng.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	// Docket has just met these pull requests, so a run starts only when
	// asked; one started by hand acts in the mode set.
	if err := eng.Tend(demo.BlockedKey); err != nil {
		t.Fatal(err)
	}
	m := waitTend(t, eng, demo.BlockedKey)

	rename, said, resolved, nilmap, ask := action(t, m, "a1"), action(t, m, "a2"), action(t, m, "a3"), action(t, m, "a4"), action(t, m, "a5")
	if rename.Verdict.Decision != model.GateAuto || rename.State != model.StateDone || rename.Patch == "" {
		t.Errorf("the rename a reviewer asked for should be pushed without asking: %+v", rename)
	}
	if said.State != model.StateDone || resolved.State != model.StateDone {
		t.Errorf("the reply about the rename and the resolve should run: %+v / %+v", said, resolved)
	}
	if nilmap.Verdict.Decision != model.GateAsk || nilmap.Verdict.Floor || nilmap.State != model.StateWaiting {
		t.Errorf("the classifier should hold the startup change for the author: %+v", nilmap)
	}
	if ask.Verdict.Decision != model.GateAsk || !ask.Verdict.Floor || ask.State != model.StateWaiting {
		t.Errorf("a reply that asks the reviewer something waits on a floor: %+v", ask)
	}
	if m.PR.HeadSHA != rename.Commit {
		t.Errorf("the branch should now point at the pushed rename: head %s, rename %s", m.PR.HeadSHA, rename.Commit)
	}
	if last := m.Conversation.Threads[0].Last(); !strings.Contains(last.Body, "currencyMeta") {
		t.Errorf("the reply did not post: %+v", last)
	}

	// The author edits the question and sends it.
	if err := eng.Decide(ctx, demo.BlockedKey, "a5", "approve", "Hourly for now. Configurable in a follow-up?", ""); err != nil {
		t.Fatal(err)
	}
	m, _ = eng.MineOne(demo.BlockedKey)
	if a := action(t, m, "a5"); a.State != model.StateDone || a.Body != "Hourly for now. Configurable in a follow-up?" {
		t.Errorf("the approved reply: %+v", a)
	}
	if err := eng.Decide(ctx, demo.BlockedKey, "a4", "explode", "", ""); err == nil {
		t.Error("an unknown choice was accepted")
	}

	// Undo the pushed rename: a revert commit on top, never a rewrite.
	if err := eng.Undo(ctx, demo.BlockedKey, "a1"); err != nil {
		t.Fatal(err)
	}
	m, _ = eng.MineOne(demo.BlockedKey)
	if a := action(t, m, "a1"); a.State != model.StateUndone {
		t.Errorf("undo: %+v", a)
	}
	if !strings.HasPrefix(m.PR.HeadSHA, "demo-revert") {
		t.Errorf("undo should push a revert commit, head is %s", m.PR.HeadSHA)
	}
}

func TestAskModeHoldsEverything(t *testing.T) {
	eng, _ := startTend(t, "ask")
	if err := eng.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := eng.Tend(demo.BlockedKey); err != nil {
		t.Fatal(err)
	}
	m := waitTend(t, eng, demo.BlockedKey)
	for _, a := range m.Tend.Actions {
		if a.State != model.StateWaiting {
			t.Errorf("in ask mode %s ran: %+v", a.ID, a)
		}
	}
}

func TestOffMeansNoRun(t *testing.T) {
	eng, _ := startTend(t, "off")
	if err := eng.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := eng.Tend(demo.BlockedKey); err == nil {
		t.Error("a run started with tending off")
	}
}

func TestADoubleClickActsOnce(t *testing.T) {
	eng, _ := startTend(t, "auto")
	ctx := context.Background()
	if err := eng.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := eng.Tend(demo.BlockedKey); err != nil {
		t.Fatal(err)
	}
	m := waitTend(t, eng, demo.BlockedKey)
	before := len(m.Conversation.Threads[1].Comments)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = eng.Decide(ctx, demo.BlockedKey, "a5", "approve", "", "")
		}()
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("exactly one approval should act: %v / %v", errs[0], errs[1])
	}
	m, _ = eng.MineOne(demo.BlockedKey)
	if got := len(m.Conversation.Threads[1].Comments); got != before+1 {
		t.Errorf("the reply posted %d times", got-before)
	}
}

func TestUndoStillWorksAfterTheNextRun(t *testing.T) {
	eng, _ := startTend(t, "auto")
	ctx := context.Background()
	if err := eng.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := eng.Tend(demo.BlockedKey); err != nil {
		t.Fatal(err)
	}
	waitTend(t, eng, demo.BlockedKey)
	if err := eng.Tend(demo.BlockedKey); err == nil {
		t.Error("a new run started while steps wait for the user")
	}
	if err := eng.Decide(ctx, demo.BlockedKey, "a5", "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	// Sending back the last waiting step starts the next run.
	if err := eng.Decide(ctx, demo.BlockedKey, "a4", "send-back", "", "Not at startup."); err != nil {
		t.Fatal(err)
	}
	m := waitTend(t, eng, demo.BlockedKey)
	if m.Tend.Run != 2 {
		t.Fatalf("run = %d, want 2", m.Tend.Run)
	}
	var first *model.TendAction
	for i := range m.Tend.Earlier {
		if m.Tend.Earlier[i].ID == "1.a1" {
			first = &m.Tend.Earlier[i]
		}
	}
	if first == nil || first.State != model.StateDone || first.Undo == "" {
		t.Fatalf("the first run's pushed rename is gone from the log: %+v", m.Tend.Earlier)
	}
	if err := eng.Undo(ctx, demo.BlockedKey, "1.a1"); err != nil {
		t.Fatal(err)
	}
	if err := eng.Undo(ctx, demo.BlockedKey, "1.a1"); err == nil {
		t.Error("the same step was undone twice")
	}
}
