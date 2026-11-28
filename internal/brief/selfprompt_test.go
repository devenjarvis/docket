package brief_test

import (
	"strings"
	"testing"

	"github.com/devenjarvis/docket/internal/brief"
)

func TestTheSelfReviewPromptSpeaksToTheAuthor(t *testing.T) {
	self := brief.Prompt(brief.PromptData{Workspace: ws, RunCode: true, Self: true, Previous: true})
	if !strings.HasPrefix(self, brief.SelfPrefix) {
		t.Fatalf("a self-review prompt should start with SelfPrefix: %.80q", self)
	}
	for _, want := range []string{"fix it before review", "tell reviewers why", "Omit `context`", "author's own rulebook", "author's earlier answer"} {
		if !strings.Contains(self, want) {
			t.Errorf("the self-review prompt should say %q", want)
		}
	}
	for _, not := range []string{"You are preparing a pull request for a human reviewer", "the reviewer's earlier answer"} {
		if strings.Contains(self, not) {
			t.Errorf("the self-review prompt should not say %q", not)
		}
	}
	review := brief.Prompt(brief.PromptData{Workspace: ws, RunCode: true})
	if strings.HasPrefix(review, brief.SelfPrefix) || strings.Contains(review, "fix it before review") {
		t.Error("a review prompt should not carry the self-review wording")
	}
	if !strings.Contains(review, `Always include an option for "I do not know, ask the author"`) {
		t.Error("a review prompt should keep its decision guidance")
	}
}
