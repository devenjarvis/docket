package model

import "testing"

func kinds(bs []Blocker) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Kind)
	}
	return out
}

func TestBlockersAndLanes(t *testing.T) {
	clean := MergeState{Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN", ReviewDecision: "APPROVED"}
	pass := Check{Name: "test", Status: "COMPLETED", Conclusion: "SUCCESS"}
	fail := Check{Name: "test", Status: "COMPLETED", Conclusion: "FAILURE"}
	pending := Check{Name: "lint", Status: "IN_PROGRESS"}
	theirs := Thread{ID: "t1", Comments: []Comment{{Author: "maya"}}}
	answered := Thread{ID: "t2", Comments: []Comment{{Author: "maya"}, {Author: "me"}}}
	resolved := Thread{ID: "t3", Resolved: true, Comments: []Comment{{Author: "maya"}}}

	with := func(f func(*MergeState)) MergeState { m := clean; f(&m); return m }
	cases := []struct {
		name   string
		draft  bool
		state  MergeState
		checks []Check
		conv   Conversation
		want   []string
		lane   string
	}{
		{name: "ready", state: clean, checks: []Check{pass}, conv: Conversation{Threads: []Thread{resolved}}, lane: LaneReady},
		{name: "draft", draft: true, state: clean, want: []string{BlockDraft}, lane: LaneNeedsYou},
		{name: "conflicts", state: with(func(m *MergeState) { m.Mergeable = "CONFLICTING"; m.MergeStateStatus = "DIRTY" }), want: []string{BlockConflicts}, lane: LaneNeedsYou},
		{name: "behind", state: with(func(m *MergeState) { m.MergeStateStatus = "BEHIND" }), want: []string{BlockBehind}, lane: LaneNeedsYou},
		{name: "failing check", state: clean, checks: []Check{pass, fail}, want: []string{BlockChecksFailing}, lane: LaneNeedsYou},
		{name: "pending only", state: clean, checks: []Check{pass, pending}, want: []string{BlockChecksPending}, lane: LaneWaiting},
		{name: "changes requested", state: with(func(m *MergeState) { m.ReviewDecision = "CHANGES_REQUESTED" }), want: []string{BlockChangesRequested}, lane: LaneNeedsYou},
		{name: "review required", state: with(func(m *MergeState) { m.ReviewDecision = "REVIEW_REQUIRED" }), want: []string{BlockReviewRequired}, lane: LaneWaiting},
		{name: "thread on you", state: clean, conv: Conversation{Threads: []Thread{theirs}}, want: []string{BlockThreads}, lane: LaneNeedsYou},
		{name: "thread answered by you", state: clean, conv: Conversation{Threads: []Thread{answered}}, want: []string{BlockThreads}, lane: LaneWaiting},
		{name: "unknown mergeability is not a blocker", state: with(func(m *MergeState) { m.Mergeable = "UNKNOWN"; m.MergeStateStatus = "UNKNOWN" }), lane: LaneReady},
		{name: "protection not met", state: with(func(m *MergeState) { m.MergeStateStatus = "BLOCKED"; m.ReviewDecision = "" }), want: []string{BlockReviewRequired}, lane: LaneWaiting},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bs := Blockers("me", c.draft, c.state, c.checks, c.conv)
			got := kinds(bs)
			if len(got) != len(c.want) {
				t.Fatalf("blockers = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("blockers = %v, want %v", got, c.want)
				}
			}
			if l := LaneOf(bs); l != c.lane {
				t.Errorf("lane = %s, want %s", l, c.lane)
			}
		})
	}
}
