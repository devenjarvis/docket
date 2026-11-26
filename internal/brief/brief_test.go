package brief

import (
	"strings"
	"testing"

	"github.com/devenjarvis/docket/internal/diff"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/owners"
	"github.com/devenjarvis/docket/internal/repo"
)

var files = []diff.File{{Path: "a.go", Hunks: []diff.Hunk{{NewStart: 1, Lines: []diff.Line{{Kind: "+", New: 1, Text: "x"}}}}}, {Path: "b.go"}}

func good() *model.Brief {
	no := false
	return &model.Brief{
		Summary:  "Does a thing.",
		Sections: []model.Section{{ID: "s1", Title: "A", Kind: model.Judgment, Files: []model.FileRange{{Path: "a.go"}}}, {ID: "s2", Title: "B", Kind: model.Mechanical, Proof: "trees equal", Files: []model.FileRange{{Path: "b.go"}}}},
		Claims:   []model.Claim{{ID: "c1", Text: "safe", Status: model.Contradicted, Evidence: "probe", Decision: "d1"}},
		Decisions: []model.Decision{{ID: "d1", Section: "s1", Title: "It is not safe.", Question: "Accept?", Rule: "R01",
			Options:  []model.Option{{ID: "a", Label: "No", Effect: model.Blocking, Comment: "Fix it."}, {ID: "b", Label: "Yes", Effect: model.Accept}},
			Anchor:   model.Anchor{Path: "a.go", Line: 1},
			Evidence: []model.Evidence{{Kind: "probe", Title: "p", Passed: &no}}}},
	}
}

func TestValidateAcceptsAGoodBrief(t *testing.T) {
	if errs := Validate(good(), files, map[string]bool{"R01": true}); len(errs) != 0 {
		t.Fatalf("unexpected problems: %v", errs)
	}
}

func TestValidateNamesEachProblem(t *testing.T) {
	cases := map[string]func(*model.Brief){
		"in no section":             func(b *model.Brief) { b.Sections = b.Sections[:1] },
		"gives no proof":            func(b *model.Brief) { b.Sections[1].Proof = "" },
		"not in the diff":           func(b *model.Brief) { b.Sections[0].Files[0].Path = "zzz.go" },
		"at least two options":      func(b *model.Brief) { b.Decisions[0].Options = b.Decisions[0].Options[:1] },
		"needs a draft comment":     func(b *model.Brief) { b.Decisions[0].Options[0].Comment = "" },
		"has no evidence":           func(b *model.Brief) { b.Decisions[0].Evidence = nil },
		"you were not given":        func(b *model.Brief) { b.Decisions[0].Rule = "R99" },
		"contradicted but has no":   func(b *model.Brief) { b.Claims[0].Decision = "" },
		"kind must be":              func(b *model.Brief) { b.Sections[0].Kind = "important" },
		"needs before, after":       func(b *model.Brief) { b.Decisions[0].BeforeAfter = &model.BeforeAfter{Kind: "timeline"} },
		"summary is empty":          func(b *model.Brief) { b.Summary = " " },
		"section \"nope\" does not": func(b *model.Brief) { b.Decisions[0].Section = "nope" },
	}
	for want, breakIt := range cases {
		b := good()
		breakIt(b)
		errs := strings.Join(Validate(b, files, map[string]bool{"R01": true}), "\n")
		if !strings.Contains(errs, want) {
			t.Errorf("expected a problem mentioning %q, got:\n%s", want, errs)
		}
	}
}

func TestParseToleratesACodeFence(t *testing.T) {
	b, err := Parse([]byte("```json\n{\"summary\": \"x\"}\n```\n"))
	if err != nil || b.Summary != "x" {
		t.Fatalf("got %+v, %v", b, err)
	}
}

func TestPromptNamesEveryDirectoryAndHonoursRunCode(t *testing.T) {
	ws := repo.Workspace{Dir: "/r", Head: "/r/head", Base: "/r/base", Scratch: "/r/scratch", Out: "/r/out"}
	on := Prompt(PromptData{Workspace: ws, RunCode: true})
	off := Prompt(PromptData{Workspace: ws, RunCode: false, Slice: true, Previous: true})
	for _, want := range []string{"/r/head", "/r/base", "/r/diff.patch", "/r/out/brief.json", "/r/scratch"} {
		if !strings.Contains(on, want) {
			t.Errorf("prompt does not mention %s", want)
		}
	}
	if !strings.Contains(off, "Do not execute code") || strings.Contains(on, "Do not execute code") {
		t.Error("the run-code setting is not reflected in the prompt")
	}
	if !strings.Contains(off, "previous.json") || !strings.Contains(off, "outside your slice") {
		t.Error("re-read and slice instructions are missing")
	}
}

func TestCodeownersSlice(t *testing.T) {
	f := owners.Parse("*            @acme/everyone\n/src/delivery/ @deven @acme/payments\ndocs/        @writer\n*.sql        @dba\n")
	paths := []string{"src/delivery/deliver.ts", "src/api/x.ts", "db/m/1.sql", "docs/a.md", "src/delivery/sub/y.ts"}
	mine, ok := f.Slice(paths, "Deven", nil)
	if !ok || len(mine) != 2 || mine[0] != "src/delivery/deliver.ts" || mine[1] != "src/delivery/sub/y.ts" {
		t.Errorf("slice = %v (%v)", mine, ok)
	}
	if o := f.Owners("db/m/1.sql"); len(o) != 1 || o[0] != "@dba" {
		t.Errorf("last matching pattern should win: %v", o)
	}
	if _, ok := f.Slice(paths, "nobody", nil); ok {
		t.Error("a reviewer the file never names has no slice")
	}
}
