package review

import (
	"strings"
	"testing"
	"time"

	"github.com/devenjarvis/docket/internal/model"
)

var inboxNow = time.Date(2026, 11, 27, 15, 0, 0, 0, time.UTC)

func rev(n int, author, lane string, minutes, decisions, answered int, found time.Duration) Summary {
	pr := model.PR{Owner: "o", Repo: "r", Number: n, Author: author, Title: "t"}
	return Summary{Key: pr.Key(), PR: pr, Status: "ready", Lane: lane, Minutes: minutes,
		Decisions: decisions, Answered: answered, Found: inboxNow.Add(-found)}
}

func own(n int, lane string, updated time.Duration, blockers ...model.Blocker) model.Mine {
	pr := model.PR{Owner: "o", Repo: "r", Number: n, Author: "me", Title: "t", BaseRef: "main", UpdatedAt: inboxNow.Add(-updated)}
	return model.Mine{PR: pr, Lane: lane, Blockers: blockers}
}

func keys(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Key)
	}
	return out
}

func find(t *testing.T, items []Item, n int) Item {
	t.Helper()
	for _, it := range items {
		if it.PR.Number == n {
			return it
		}
	}
	t.Fatalf("no row for #%d", n)
	return Item{}
}

func TestInboxBandsAndOrder(t *testing.T) {
	reviews := []Summary{
		rev(1, "ana", "judgment", 18, 3, 0, 2*time.Hour),    // closer
		rev(2, "ben", "clear", 2, 0, 0, time.Hour),          // skim
		rev(3, "chen", "judgment", 3, 2, 2, 3*time.Hour),    // quick: only needs posting
		rev(4, "dmitri", "judgment", 6, 1, 0, 50*time.Hour), // closer, waited two days
	}
	mine := []model.Mine{
		own(10, model.LaneReady, time.Hour), // quick: merge
		own(11, model.LaneNeedsYou, time.Hour, model.Blocker{Kind: model.BlockConflicts, Text: "It conflicts.", OnYou: true}),
	}
	got := keys(BuildInbox("me", reviews, mine, nil, inboxNow))
	want := []string{"o/r#3", "o/r#10", "o/r#4", "o/r#11", "o/r#1", "o/r#2"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("order:\n got %v\nwant %v", got, want)
	}
}

func TestInboxReviewerOfYoursComesFirst(t *testing.T) {
	m := own(20, model.LaneWaiting, time.Hour, model.Blocker{Kind: model.BlockReviewRequired, Text: "x"})
	m.State.RequestedReviewers = []string{"ana"}
	reviews := []Summary{
		rev(1, "ben", "judgment", 4, 1, 0, 2*time.Hour),
		rev(2, "ana", "judgment", 20, 1, 0, time.Hour),
	}
	items := BuildInbox("me", reviews, []model.Mine{m}, nil, inboxNow)
	if items[0].PR.Number != 2 {
		t.Fatalf("ana's review should lead, got %v", keys(items))
	}
	if !strings.Contains(items[0].Why, "ana is reviewing your #20.") {
		t.Errorf("why = %q", items[0].Why)
	}
}

func TestInboxNudgeAfterTwoDays(t *testing.T) {
	fresh := own(30, model.LaneWaiting, 5*time.Hour, model.Blocker{Kind: model.BlockReviewRequired, Text: "x"})
	stale := own(31, model.LaneWaiting, 72*time.Hour, model.Blocker{Kind: model.BlockReviewRequired, Text: "x"})
	stale.State.RequestedReviewers = []string{"dmitri"}
	items := BuildInbox("me", nil, []model.Mine{fresh, stale}, nil, inboxNow)
	f, s := find(t, items, 30), find(t, items, 31)
	if f.Band != BandWaiting || f.Status != "Waiting for review" {
		t.Errorf("fresh: band %q status %q", f.Band, f.Status)
	}
	if s.Band != BandQuick || s.Action != "Nudge" {
		t.Errorf("stale: band %q action %q", s.Band, s.Action)
	}
	if !strings.Contains(s.Why, "3 days") || !strings.Contains(s.Why, "dmitri") {
		t.Errorf("stale why = %q", s.Why)
	}
}

func TestInboxAgentWork(t *testing.T) {
	held := own(40, model.LaneNeedsYou, time.Hour, model.Blocker{Kind: model.BlockThreads, Text: "x", OnYou: true})
	held.Tend = &model.Tend{Status: "ready", Actions: []model.TendAction{
		{Kind: model.ActFix, State: model.StateWaiting, Verdict: &model.Verdict{Reason: "It changes a config default"}},
		{Kind: model.ActReply, State: model.StateDone},
	}}
	running := own(41, model.LaneNeedsYou, time.Hour, model.Blocker{Kind: model.BlockChecksFailing, Text: "1 check failing.", OnYou: true})
	running.Tend = &model.Tend{Status: "running", Activity: "Running the tests"}
	items := BuildInbox("me", nil, []model.Mine{held, running}, nil, inboxNow)
	h, r := find(t, items, 40), find(t, items, 41)
	if h.Band != BandQuick || h.Action != "Check fix" || !strings.Contains(h.Why, "It changes a config default.") {
		t.Errorf("held: %+v", h)
	}
	if r.Band != BandWaiting || !r.Live || r.Activity != "Running the tests" {
		t.Errorf("running: %+v", r)
	}
}

func TestInboxReviewStates(t *testing.T) {
	posted := rev(1, "ana", "done", 0, 0, 0, time.Hour)
	failed := rev(2, "ben", "failed", 0, 0, 0, time.Hour)
	failed.Error = "agent exited\nstack"
	reading := rev(3, "chen", "reading", 0, 0, 0, time.Hour)
	reread := rev(4, "dmitri", "judgment", 9, 2, 2, time.Hour)
	reread.Reread = true
	items := BuildInbox("me", []Summary{posted, failed, reading, reread}, nil, nil, inboxNow)
	if it := find(t, items, 1); it.Band != BandDone {
		t.Errorf("posted band %q", it.Band)
	}
	if it := find(t, items, 2); it.Band != BandWaiting || it.Why != "agent exited" {
		t.Errorf("failed: %+v", it)
	}
	if it := find(t, items, 3); it.Band != BandWaiting || !it.Live {
		t.Errorf("reading: %+v", it)
	}
	if it := find(t, items, 4); it.Band != BandQuick || it.Action != "Re-review" || it.Minutes != 5 {
		t.Errorf("reread: %+v", it)
	}
}

func TestInboxChangesRequestedNamesReviewer(t *testing.T) {
	m := own(50, model.LaneNeedsYou, time.Hour, model.Blocker{Kind: model.BlockChangesRequested, Text: "x", OnYou: true})
	m.Conversation.Reviews = []model.ReviewEvent{
		{Author: "ana", State: "APPROVED", At: inboxNow.Add(-2 * time.Hour)},
		{Author: "ben", State: "CHANGES_REQUESTED", At: inboxNow.Add(-time.Hour)},
	}
	it := find(t, BuildInbox("me", nil, []model.Mine{m}, nil, inboxNow), 50)
	if it.Action != "Respond" || it.Waiter != "ben" || it.Why != "ben asked for changes." {
		t.Errorf("%+v", it)
	}
}

func TestSnoozeWakes(t *testing.T) {
	pr := model.PR{HeadSHA: "a", UpdatedAt: inboxNow}
	change := Snooze{Kind: SnoozeChange, Head: "a", Updated: inboxNow}
	if change.Awake(pr, inboxNow.Add(time.Hour)) {
		t.Error("a change snooze woke with nothing changed")
	}
	if !change.Awake(model.PR{HeadSHA: "b", UpdatedAt: inboxNow}, inboxNow) {
		t.Error("a change snooze slept through a push")
	}
	if !change.Awake(model.PR{HeadSHA: "a", UpdatedAt: inboxNow.Add(time.Minute)}, inboxNow) {
		t.Error("a change snooze slept through a comment")
	}
	until := tomorrowMorning(inboxNow)
	if until.Day() != inboxNow.Day()+1 || until.Hour() != 8 {
		t.Errorf("tomorrow morning = %v", until)
	}
	tomorrow := Snooze{Kind: SnoozeTomorrow, Until: until}
	if tomorrow.Awake(pr, until.Add(-time.Minute)) || !tomorrow.Awake(pr, until) {
		t.Error("a tomorrow snooze ends at 8 the next morning")
	}
}

func TestInboxSnoozedRowsWait(t *testing.T) {
	r := rev(1, "ana", "judgment", 5, 1, 0, time.Hour)
	m := own(2, model.LaneReady, time.Hour)
	snoozes := map[string]Snooze{
		r.Key:      {Kind: SnoozeTomorrow, Until: inboxNow.Add(time.Hour)},
		m.PR.Key(): {Kind: SnoozeChange, Updated: m.PR.UpdatedAt},
	}
	items := BuildInbox("me", []Summary{r}, []model.Mine{m}, snoozes, inboxNow)
	for _, it := range items {
		if it.Band != BandWaiting || it.Status != "Snoozed" || it.Snoozed == "" {
			t.Errorf("#%d: %+v", it.PR.Number, it)
		}
	}
	// Once tomorrow comes, the review is back where it was.
	items = BuildInbox("me", []Summary{r}, nil, snoozes, inboxNow.Add(2*time.Hour))
	if items[0].Band != BandCloser || items[0].Snoozed != "" {
		t.Errorf("after the snooze: %+v", items[0])
	}
}
