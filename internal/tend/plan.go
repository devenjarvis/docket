// Package tend is the agent's work on the user's own pull requests: the
// prompt that asks for a plan, the check that the plan holds up, and the gate
// that decides which of its steps may run without asking.
package tend

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/devenjarvis/docket/internal/model"
)

//go:embed prompt.md
var promptText string

var promptTmpl = template.Must(template.New("tend").Parse(promptText))

// PromptPrefix starts every tend prompt, so a scripted agent can tell it
// from the reading and asking prompts.
const PromptPrefix = "You are working for the author of a pull request"

// PromptData fills the tend prompt.
type PromptData struct {
	// Dir holds the inputs: pr.json, conversation.json, checks.json and,
	// when the user sent something back, notes.json.
	Dir     string
	Work    string
	Scratch string
	Out     string
	Head    string
	Login   string
	RunCode bool
	Notes   bool
	// Tasks is set when the author handed over self-review findings, in
	// tasks.json.
	Tasks bool
}

// Prompt is the instruction for one tend run.
func Prompt(d PromptData) string {
	var b strings.Builder
	if err := promptTmpl.Execute(&b, d); err != nil {
		panic(err) // the template is embedded; a failure is a build error
	}
	return b.String()
}

// RetryPrompt sends a plan back with what was wrong with it.
func RetryPrompt(out string, problems []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The file at %s/plan.json cannot be used yet. Fix these and write it again, complete:\n\n", out)
	for _, p := range problems {
		fmt.Fprintf(&b, "- %s\n", p)
	}
	b.WriteString("\nChange only what these point at. Keep the commits you already made if they are still right.")
	return b.String()
}

// Parse decodes a plan, tolerating a Markdown code fence around it.
func Parse(b []byte) (*model.TendPlan, error) {
	s := strings.TrimSpace(string(b))
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	var p model.TendPlan
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		return nil, fmt.Errorf("plan.json is not valid JSON: %w", err)
	}
	return &p, nil
}

var kinds = map[string]bool{
	model.ActFix: true, model.ActReply: true, model.ActResolve: true,
	model.ActUpdateBranch: true, model.ActRerun: true, model.ActMergeBase: true,
}

var stances = map[string]bool{"agrees": true, "informs": true, "asks": true, "declines": true}

// Validate returns every problem with a plan, in words the agent can act
// on. threads, checks and tasks are the ids that exist; onTop reports
// whether a commit descends from the head the agent was given.
func Validate(p *model.TendPlan, threads, checks, tasks map[string]bool, onTop func(sha string) bool) []string {
	var probs []string
	bad := func(format string, args ...any) { probs = append(probs, fmt.Sprintf(format, args...)) }
	ids := map[string]bool{}
	for i, a := range p.Actions {
		where := fmt.Sprintf("actions[%d] (%s)", i, a.ID)
		if a.ID == "" {
			bad("%s has no id", where)
		} else if ids[a.ID] {
			bad("%s: the id %s is used twice", where, a.ID)
		}
		ids[a.ID] = true
		if !kinds[a.Kind] {
			bad("%s: kind %q is not one of fix, reply, resolve, update-branch, rerun, merge-base", where, a.Kind)
			continue
		}
		for _, t := range a.Threads {
			if !threads[t] {
				bad("%s: there is no thread %s in conversation.json", where, t)
			}
		}
		for _, c := range a.Checks {
			if !checks[c] {
				bad("%s: there is no check %s in checks.json", where, c)
			}
		}
		for _, t := range a.Tasks {
			if !tasks[t] {
				bad("%s: there is no finding %s in tasks.json", where, t)
			}
		}
		switch a.Kind {
		case model.ActFix, model.ActMergeBase:
			switch {
			case a.Commit == "":
				bad("%s: a %s needs the commit it made", where, a.Kind)
			case !onTop(a.Commit):
				bad("%s: commit %s is not on top of the head you were given; commit in the checkout, on its branch", where, a.Commit)
			}
			if a.Kind == model.ActFix && len(a.Threads)+len(a.Checks)+len(a.Tasks) == 0 {
				bad("%s: say which threads, checks or findings this fix answers", where)
			}
		case model.ActReply, model.ActResolve:
			if !threads[a.Thread] {
				bad("%s: there is no thread %q in conversation.json", where, a.Thread)
			}
			if a.Kind == model.ActReply {
				if strings.TrimSpace(a.Body) == "" {
					bad("%s: a reply needs a body", where)
				}
				if !stances[a.Stance] {
					bad("%s: a reply needs a stance: agrees, informs, asks or declines", where)
				}
			}
		case model.ActRerun:
			if len(a.Checks) == 0 {
				bad("%s: name the failed check to re-run", where)
			}
		}
	}
	return probs
}
