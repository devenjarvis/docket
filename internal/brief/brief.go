// Package brief checks what an agent hands back about a pull request and
// fills in what Docket can work out for itself. A brief that fails Validate
// goes back to the agent with the list of problems.
package brief

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/devenjarvis/docket/internal/diff"
	"github.com/devenjarvis/docket/internal/model"
)

// Parse decodes a brief, tolerating a Markdown code fence around it.
func Parse(b []byte) (*model.Brief, error) {
	s := strings.TrimSpace(string(b))
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	var out model.Brief
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("brief.json is not valid JSON: %w", err)
	}
	return &out, nil
}

func oneOf(v string, set ...string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}

// Validate returns every problem with a brief, in words the agent can act
// on. rules lists the ids of the rules the agent was given.
func Validate(b *model.Brief, files []diff.File, rules map[string]bool) []string {
	var errs []string
	bad := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	if strings.TrimSpace(b.Summary) == "" {
		bad("summary is empty")
	}
	if len(b.Sections) == 0 {
		bad("sections is empty: every changed file must appear in a section")
	}
	secs := map[string]bool{}
	covered := map[string]bool{}
	for i, s := range b.Sections {
		if s.ID == "" {
			bad("sections[%d] has no id", i)
		}
		if secs[s.ID] {
			bad("section id %q is used twice", s.ID)
		}
		secs[s.ID] = true
		if !oneOf(s.Kind, model.Judgment, model.Skim, model.Mechanical) {
			bad("section %q: kind must be judgment, skim or mechanical, not %q", s.ID, s.Kind)
		}
		if strings.TrimSpace(s.Title) == "" {
			bad("section %q has no title", s.ID)
		}
		if s.Kind != model.Judgment && strings.TrimSpace(s.Proof) == "" {
			bad("section %q is %s but gives no proof: say what you ran or compared, or make it judgment", s.ID, s.Kind)
		}
		if len(s.Files) == 0 {
			bad("section %q lists no files", s.ID)
		}
		for _, f := range s.Files {
			if _, ok := diff.Find(files, f.Path); !ok {
				bad("section %q names %q, which is not in the diff", s.ID, f.Path)
				continue
			}
			covered[f.Path] = true
			for _, r := range f.Ranges {
				if r[0] <= 0 || r[1] < r[0] {
					bad("section %q: range %v in %q is not [start, end] with 1 <= start <= end", s.ID, r, f.Path)
				}
			}
		}
	}
	var missing []string
	for _, f := range files {
		if !covered[f.Path] {
			missing = append(missing, f.Path)
		}
	}
	if len(missing) > 0 {
		if len(missing) > 12 {
			missing = append(missing[:12], fmt.Sprintf("and %d more", len(missing)-12))
		}
		bad("these changed files are in no section: %s", strings.Join(missing, ", "))
	}

	decs := map[string]bool{}
	for i, d := range b.Decisions {
		if d.ID == "" {
			bad("decisions[%d] has no id", i)
		}
		if decs[d.ID] {
			bad("decision id %q is used twice", d.ID)
		}
		decs[d.ID] = true
		if !secs[d.Section] {
			bad("decision %q: section %q does not exist", d.ID, d.Section)
		}
		if strings.TrimSpace(d.Title) == "" || strings.TrimSpace(d.Question) == "" {
			bad("decision %q needs both a title (the finding, as a fact) and a question (what the reviewer must decide)", d.ID)
		}
		if n := len([]rune(strings.TrimSpace(d.Headline))); n > 48 {
			bad("decision %q: the headline is %d characters; keep it to a few words, at most 48", d.ID, n)
		}
		if len(d.Options) < 2 {
			bad("decision %q needs at least two options", d.ID)
		}
		opts := map[string]bool{}
		for _, o := range d.Options {
			if o.ID == "" || opts[o.ID] {
				bad("decision %q: option ids must be present and unique", d.ID)
			}
			opts[o.ID] = true
			if !oneOf(o.Effect, model.Blocking, model.Accept, model.Ask) {
				bad("decision %q option %q: effect must be blocking, accept or ask, not %q", d.ID, o.ID, o.Effect)
			}
			if o.Effect != model.Accept && strings.TrimSpace(o.Comment) == "" {
				bad("decision %q option %q: a %s option needs a draft comment", d.ID, o.ID, o.Effect)
			}
		}
		if len(d.Evidence) == 0 {
			bad("decision %q has no evidence: show what you ran or read", d.ID)
		}
		if d.Anchor.Path != "" {
			if _, ok := diff.Find(files, d.Anchor.Path); !ok {
				bad("decision %q: anchor path %q is not in the diff", d.ID, d.Anchor.Path)
			}
		}
		for _, h := range d.Hot {
			if _, ok := diff.Find(files, h.Path); !ok {
				bad("decision %q: hot path %q is not in the diff", d.ID, h.Path)
			}
		}
		if d.Rule != "" && d.Rule != "repo" && !rules[d.Rule] {
			bad("decision %q cites rule %q, which you were not given", d.ID, d.Rule)
		}
		if ba := d.BeforeAfter; ba != nil {
			errs = append(errs, checkBA("decision "+d.ID, ba)...)
		}
	}
	for _, c := range b.Claims {
		if !oneOf(c.Status, model.Proved, model.Contradicted, model.Unproven, model.Unstated) {
			bad("claim %q: status must be proved, contradicted, unproven or unstated, not %q", c.ID, c.Status)
		}
		if strings.TrimSpace(c.Evidence) == "" {
			bad("claim %q has no evidence", c.ID)
		}
		if c.Decision != "" && !decs[c.Decision] {
			bad("claim %q points at decision %q, which does not exist", c.ID, c.Decision)
		}
		if c.Status == model.Contradicted && c.Decision == "" {
			bad("claim %q is contradicted but has no decision: the reviewer has to rule on it", c.ID)
		}
	}
	for _, h := range b.RuleHits {
		if h.Rule != "repo" && !rules[h.Rule] {
			bad("ruleHits cites rule %q, which you were not given", h.Rule)
		}
		if !oneOf(h.Result, "violated", "passed", "unclear") {
			bad("ruleHits %q: result must be violated, passed or unclear", h.Rule)
		}
	}
	if b.Context != nil && b.Context.BeforeAfter != nil {
		errs = append(errs, checkBA("context", b.Context.BeforeAfter)...)
	}
	return errs
}

func checkBA(where string, ba *model.BeforeAfter) []string {
	var errs []string
	switch ba.Kind {
	case "timeline":
		if ba.Before == nil || ba.After == nil || ba.Max <= 0 {
			errs = append(errs, where+": a timeline beforeAfter needs before, after and a max greater than 0")
			break
		}
		for name, tl := range map[string]*model.Timeline{"before": ba.Before, "after": ba.After} {
			for _, l := range tl.Lanes {
				for _, s := range l.Spans {
					if s.To < s.From || s.From < 0 || s.To > ba.Max {
						errs = append(errs, fmt.Sprintf("%s: %s lane %q has a span outside 0..max", where, name, l.Label))
					}
				}
			}
		}
	case "table":
		if len(ba.Rows) == 0 {
			errs = append(errs, where+": a table beforeAfter needs rows")
		}
	case "text", "image":
		if ba.BeforeText == "" && ba.AfterText == "" {
			errs = append(errs, where+": a "+ba.Kind+" beforeAfter needs beforeText and afterText")
		}
	default:
		errs = append(errs, fmt.Sprintf("%s: beforeAfter kind must be timeline, table, text or image, not %q", where, ba.Kind))
	}
	return errs
}

// Fill computes what the agent should not be trusted to count: the changed
// lines in each section.
func Fill(b *model.Brief, files []diff.File) {
	for i := range b.Sections {
		n := 0
		for _, fr := range b.Sections[i].Files {
			if f, ok := diff.Find(files, fr.Path); ok {
				n += diff.Changed(diff.Slice(f, fr.Ranges, 0))
			}
		}
		b.Sections[i].Lines = n
	}
}

// Lead is one finding a lens hands the lead reader.
type Lead struct {
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	Severity string `json:"severity,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

// MaxLeads is how many findings Docket takes from one lens.
const MaxLeads = 8

// ParseLeads decodes a lens's findings file, tolerating a code fence. Leads
// with no title are dropped, and at most MaxLeads are kept.
func ParseLeads(b []byte) ([]Lead, error) {
	s := strings.TrimSpace(string(b))
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	var out struct {
		Findings []Lead `json:"findings"`
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("findings.json is not valid JSON: %w", err)
	}
	leads := []Lead{}
	for _, l := range out.Findings {
		if strings.TrimSpace(l.Title) == "" {
			continue
		}
		if !oneOf(l.Severity, "high", "medium", "low") {
			l.Severity = "medium"
		}
		leads = append(leads, l)
		if len(leads) == MaxLeads {
			break
		}
	}
	return leads, nil
}

// ValidateLenses returns a problem for each decision whose lens is not one
// of the lenses that ran.
func ValidateLenses(b *model.Brief, ids map[string]bool) []string {
	var errs []string
	for _, d := range b.Decisions {
		if d.Lens != "" && !ids[d.Lens] {
			names := make([]string, 0, len(ids))
			for id := range ids {
				names = append(names, id)
			}
			sort.Strings(names)
			if len(names) == 0 {
				errs = append(errs, fmt.Sprintf("decision %q: lens %q did not run; leave lens empty", d.ID, d.Lens))
			} else {
				errs = append(errs, fmt.Sprintf("decision %q: lens %q is not one of the lenses that ran (%s)", d.ID, d.Lens, strings.Join(names, ", ")))
			}
		}
	}
	return errs
}
