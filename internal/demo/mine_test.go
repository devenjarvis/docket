package demo

import (
	"context"
	"testing"

	"github.com/devenjarvis/docket/internal/model"
)

func TestDemoInboxActionsChangeWhatComesBack(t *testing.T) {
	ctx := context.Background()
	g := GitHub{Home: t.TempDir()}
	mine, err := g.Mine(ctx)
	if err != nil || len(mine) != 4 {
		t.Fatalf("mine = %d (%v), want 4", len(mine), err)
	}
	lanes := map[string]string{}
	for _, p := range mine {
		full, err := g.PR(ctx, p.Owner, p.Repo, p.Number)
		if err != nil {
			t.Fatal(err)
		}
		st, checks, conv, err := g.Detail(ctx, p.Owner, p.Repo, p.Number)
		if err != nil {
			t.Fatal(err)
		}
		lanes[p.Key()] = model.LaneOf(model.Blockers("devenjarvis", full.Draft, st, checks, conv))
	}
	if lanes[ReadyKey] != model.LaneReady || lanes[BlockedKey] != model.LaneNeedsYou || lanes[ConflictKey] != model.LaneNeedsYou || lanes[DraftKey] != model.LaneNeedsYou {
		t.Fatalf("lanes = %v", lanes)
	}

	blocked, _ := g.PR(ctx, "acme", "ledger", 318)
	_, _, conv, _ := g.Detail(ctx, "acme", "ledger", 318)
	tid := conv.Threads[0].ID
	if _, err := g.Reply(ctx, blocked, tid, "Done."); err != nil {
		t.Fatal(err)
	}
	_, _, conv, _ = g.Detail(ctx, "acme", "ledger", 318)
	if last := conv.Threads[0].Last(); last.Body != "Done." || last.Author != "devenjarvis" {
		t.Errorf("reply did not land: %+v", last)
	}
	if err := g.Merge(ctx, blocked, "SQUASH", blocked.HeadSHA); err == nil {
		t.Error("merged a pull request with failing checks")
	}

	ready, _ := g.PR(ctx, "acme", "payments-api", 4133)
	if err := g.Merge(ctx, ready, "SQUASH", ready.HeadSHA); err != nil {
		t.Fatal(err)
	}
	if mine, _ := g.Mine(ctx); len(mine) != 3 {
		t.Errorf("a merged pull request is still listed: %d", len(mine))
	}
	if _, err := g.FailedLog(ctx, blocked, conv2job(t, g)); err != nil {
		t.Error(err)
	}
}

func conv2job(t *testing.T, g GitHub) int64 {
	_, checks, _, _ := g.Detail(context.Background(), "acme", "ledger", 318)
	for _, c := range checks {
		if c.Failed() {
			return c.JobID
		}
	}
	t.Fatal("the blocked demo PR has no failing check")
	return 0
}
