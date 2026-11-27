package review

import (
	"context"
	"testing"

	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/repo"
)

func TestSayingDoneNeedsAFixThatRunsAlone(t *testing.T) {
	e := New(t.TempDir(), nil, nil, nil)
	ctx := context.Background()
	resolve := model.TendAction{ID: "a1", Kind: model.ActResolve, Thread: "T1", Why: "done"}
	agree := model.TendAction{ID: "a1", Kind: model.ActReply, Thread: "T1", Stance: "agrees", Body: "Fixed."}
	for _, a := range []model.TendAction{resolve, agree} {
		// No client: a floor must decide before the classifier is reached.
		v := e.judge(ctx, nil, repo.Branch{}, TendAuto, model.Mine{}, []model.TendAction{a}, 0, "")
		if v.Decision != model.GateAsk || !v.Floor {
			t.Errorf("%s with no fix in the plan: %+v", a.Kind, v)
		}
	}
	other := model.TendAction{ID: "a0", Kind: model.ActFix, Threads: []string{"T2"}, Verdict: &model.Verdict{Decision: model.GateAuto}}
	if v := e.judge(ctx, nil, repo.Branch{}, TendAuto, model.Mine{}, []model.TendAction{other, resolve}, 1, ""); v.Decision != model.GateAsk {
		t.Errorf("a fix for another thread let the resolve through: %+v", v)
	}
}

func TestARunCutShortByARestartIsMarkedFailed(t *testing.T) {
	home := t.TempDir()
	pr := model.PR{Owner: "o", Repo: "r", Number: 1}
	e := New(home, nil, nil, nil)
	m := model.Mine{PR: pr, Tend: &model.Tend{Status: "running", Actions: []model.TendAction{{ID: "a1", State: model.StateWorking}},
		Earlier: []model.TendAction{{ID: "1.a1", State: model.StateWorking}}}}
	if err := config.WriteJSON(e.minePath(pr), m); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	e.loadMineLocked()
	got := e.mine[pr.Key()]
	e.mu.Unlock()
	if got.Tend.Status != "failed" || got.Tend.Actions[0].State != model.StateFailed || got.Tend.Earlier[0].State != model.StateFailed {
		t.Errorf("after a restart: %+v", got.Tend)
	}
}

func TestAPushNeverCarriesAHeldFix(t *testing.T) {
	acts := []model.TendAction{
		{ID: "a1", Kind: model.ActFix, State: model.StateWaiting},
		{ID: "a2", Kind: model.ActReply, State: model.StateWaiting},
		{ID: "a3", Kind: model.ActFix, State: model.StateWaiting},
	}
	if got := unpushedBefore(acts, "a3"); got != "a1" {
		t.Errorf("unpushedBefore = %q, want a1", got)
	}
	acts[0].State = model.StateDone
	if got := unpushedBefore(acts, "a3"); got != "" {
		t.Errorf("with a1 pushed: %q", got)
	}
}

func TestWhenARunStartsOnItsOwn(t *testing.T) {
	e := New(t.TempDir(), nil, nil, nil)
	e.Branches = stubBrancher{}
	pr := model.PR{Owner: "o", Repo: "r", Number: 1, HeadSHA: "h1"}
	thread := func(id, author string) model.Thread {
		return model.Thread{ID: id, Comments: []model.Comment{{ID: id + "c", Author: author}}}
	}
	mine := func(threads ...model.Thread) *model.Mine {
		return &model.Mine{PR: pr, Conversation: model.Conversation{Threads: threads}}
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	first := mine(thread("T1", "sam"))
	if e.maybeTendLocked("me", nil, first) {
		t.Error("a pull request seen for the first time started a run")
	}
	same := mine(thread("T1", "sam"))
	if e.maybeTendLocked("me", first, same) {
		t.Error("nothing new started a run")
	}
	fresh := mine(thread("T1", "sam"), thread("T2", "lee"))
	if !e.maybeTendLocked("me", same, fresh) {
		t.Error("a new thread did not start a run")
	}

	capped := mine(thread("T1", "sam"), thread("T2", "lee"), thread("T3", "kim"))
	fresh.AutoRuns = maxAutoRuns
	if e.maybeTendLocked("me", fresh, capped) {
		t.Error("a run started past the cap")
	}
	if capped.Seen != fresh.Seen {
		t.Error("an item was marked seen though no run covered it")
	}

	fresh.AutoRuns = 0
	fresh.Tend = &model.Tend{Actions: []model.TendAction{{ID: "a1", State: model.StateWaiting}}}
	if e.maybeTendLocked("me", fresh, mine(thread("T1", "sam"), thread("T2", "lee"), thread("T4", "kim"))) {
		t.Error("a run started while steps wait for the user")
	}
}

type stubBrancher struct{ repo.Brancher }
