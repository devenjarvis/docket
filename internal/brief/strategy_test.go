package brief_test

import (
	"strings"
	"testing"

	"github.com/devenjarvis/docket/internal/brief"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/repo"
)

var ws = repo.Workspace{Dir: "/w", Head: "/w/head", Base: "/w/base", Scratch: "/w/scratch", Out: "/w/out"}

func TestTheLeadPromptMentionsLensesAndInstructionsOnlyWhenThereAreSome(t *testing.T) {
	plain := brief.Prompt(brief.PromptData{Workspace: ws, RunCode: true})
	for _, not := range []string{"lenses.json", `"lens"`, "Standing instructions"} {
		if strings.Contains(plain, not) {
			t.Errorf("a single read's prompt should not mention %q", not)
		}
	}
	lead := brief.Prompt(brief.PromptData{Workspace: ws, RunCode: true, Lenses: true, Instructions: "Flag every new SQL query."})
	for _, want := range []string{"/w/lenses.json", "Treat every finding as a lead", "Dropped lead", `"lens": ""`, "## Standing instructions", "Flag every new SQL query."} {
		if !strings.Contains(lead, want) {
			t.Errorf("the lead prompt should say %q", want)
		}
	}
}

func TestTheLensPromptIsReadOnlyAndCarriesItsFocus(t *testing.T) {
	p := brief.LensPrompt(brief.LensData{Workspace: ws, Name: "Security", Text: "Look for injection.", File: "/w/lenses/security/findings.json"})
	if !strings.HasPrefix(p, brief.LensPrefix) {
		t.Fatalf("a lens prompt should start with LensPrefix: %.60q", p)
	}
	for _, want := range []string{"**Security**", "Look for injection.", "`/w/lenses/security/findings.json`", "Do not execute anything", "for a human reviewer"} {
		if !strings.Contains(p, want) {
			t.Errorf("the lens prompt should say %q", want)
		}
	}
	self := brief.LensPrompt(brief.LensData{Workspace: ws, Name: "Tests", Text: "x", File: "/f", Self: true})
	if !strings.Contains(self, "for its own author") || strings.Contains(self, "reviewer's rulebook") {
		t.Error("a self-review lens should speak of the author")
	}
}

func TestTheCriticPrompt(t *testing.T) {
	p := brief.CriticPrompt(brief.CriticData{Workspace: ws, RunCode: false})
	if !strings.HasPrefix(p, brief.CriticPrefix) {
		t.Fatalf("a critic prompt should start with CriticPrefix: %.60q", p)
	}
	for _, want := range []string{"/w/out/brief.json", "first-brief.json", "Critic removed", "Do not execute code", "same ids"} {
		if !strings.Contains(p, want) {
			t.Errorf("the critic prompt should say %q", want)
		}
	}
}

func TestParseLeads(t *testing.T) {
	leads, err := brief.ParseLeads([]byte("```json\n{\"findings\": [{\"title\": \"A\", \"severity\": \"high\"}, {\"title\": \"\"}, {\"title\": \"B\", \"severity\": \"urgent\"}]}\n```"))
	if err != nil || len(leads) != 2 || leads[1].Severity != "medium" {
		t.Fatalf("leads = %+v, %v", leads, err)
	}
	many := `{"findings": [` + strings.TrimSuffix(strings.Repeat(`{"title": "x"},`, 20), ",") + `]}`
	if leads, _ := brief.ParseLeads([]byte(many)); len(leads) != brief.MaxLeads {
		t.Errorf("kept %d leads, want at most %d", len(leads), brief.MaxLeads)
	}
	if _, err := brief.ParseLeads([]byte("not json")); err == nil {
		t.Error("parsed a findings file that is not JSON")
	}
}

func TestValidateLenses(t *testing.T) {
	b := &model.Brief{Decisions: []model.Decision{{ID: "d1", Lens: "security"}, {ID: "d2", Lens: "made-up"}, {ID: "d3"}}}
	errs := brief.ValidateLenses(b, map[string]bool{"security": true, "tests": true})
	if len(errs) != 1 || !strings.Contains(errs[0], `"made-up"`) || !strings.Contains(errs[0], "security, tests") {
		t.Errorf("errs = %v", errs)
	}
	if errs := brief.ValidateLenses(b, nil); len(errs) != 2 {
		t.Errorf("with no lenses run, every lens is wrong: %v", errs)
	}
}
