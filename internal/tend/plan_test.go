package tend

import (
	"strings"
	"testing"

	"github.com/devenjarvis/docket/internal/model"
)

func TestValidate(t *testing.T) {
	threads := map[string]bool{"T1": true, "T2": true}
	checks := map[string]bool{"C1": true}
	tasks := map[string]bool{"d1": true}
	onTop := func(sha string) bool { return sha == "aaa" || sha == "bbb" }
	good := func() *model.TendPlan {
		return &model.TendPlan{Summary: "Rename and reply.", Actions: []model.TendAction{
			{ID: "a1", Kind: model.ActFix, Commit: "aaa", Threads: []string{"T1"}, Why: "rename"},
			{ID: "a2", Kind: model.ActReply, Thread: "T1", Body: "Renamed in aaa.", Stance: "agrees"},
			{ID: "a3", Kind: model.ActResolve, Thread: "T1"},
			{ID: "a4", Kind: model.ActRerun, Checks: []string{"C1"}, Why: "flaky network"},
			{ID: "a5", Kind: model.ActFix, Commit: "bbb", Tasks: []string{"d1"}, Why: "the finding the author handed over"},
		}}
	}
	if probs := Validate(good(), threads, checks, tasks, onTop); len(probs) != 0 {
		t.Fatalf("a good plan has problems: %v", probs)
	}
	cases := []struct {
		name   string
		break_ func(p *model.TendPlan)
		want   string
	}{
		{"unknown kind", func(p *model.TendPlan) { p.Actions[0].Kind = "deploy" }, "kind"},
		{"unknown thread", func(p *model.TendPlan) { p.Actions[1].Thread = "T9" }, "T9"},
		{"fix with no commit", func(p *model.TendPlan) { p.Actions[0].Commit = "" }, "commit"},
		{"commit not on top", func(p *model.TendPlan) { p.Actions[0].Commit = "zzz" }, "zzz"},
		{"fix answers nothing", func(p *model.TendPlan) { p.Actions[0].Threads = nil }, "answers"},
		{"duplicate id", func(p *model.TendPlan) { p.Actions[1].ID = "a1" }, "a1"},
		{"empty reply", func(p *model.TendPlan) { p.Actions[1].Body = " " }, "body"},
		{"reply without stance", func(p *model.TendPlan) { p.Actions[1].Stance = "" }, "stance"},
		{"unknown check", func(p *model.TendPlan) { p.Actions[3].Checks = []string{"C9"} }, "C9"},
		{"unknown finding", func(p *model.TendPlan) { p.Actions[4].Tasks = []string{"d9"} }, "d9"},
		{"finding fix answers nothing", func(p *model.TendPlan) { p.Actions[4].Tasks = nil }, "answers"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := good()
			c.break_(p)
			probs := Validate(p, threads, checks, tasks, onTop)
			if len(probs) != 1 || !strings.Contains(probs[0], c.want) {
				t.Errorf("problems = %q, want one mentioning %q", probs, c.want)
			}
		})
	}
}

func TestParseToleratesAFence(t *testing.T) {
	p, err := Parse([]byte("```json\n{\"summary\":\"s\",\"actions\":[]}\n```"))
	if err != nil || p.Summary != "s" {
		t.Fatalf("p=%+v err=%v", p, err)
	}
}
