package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/diff"
	"github.com/devenjarvis/docket/internal/gh"
	"github.com/devenjarvis/docket/internal/model"
)

// Draft is the review as it would be posted, plus why Approve is or is not
// available.
type Draft struct {
	// Event is the verdict Docket suggests: REQUEST_CHANGES when anything is
	// blocking, COMMENT when a question is open, APPROVE when neither.
	Event    string         `json:"event"`
	Body     string         `json:"body"`
	Comments []DraftComment `json:"comments"`
	// Locks lists what stands between the reviewer and Approve. Empty means
	// Approve is open.
	Locks []string `json:"locks"`
	// Warnings are things worth knowing that do not lock anything.
	Warnings []string      `json:"warnings"`
	Receipt  []ReceiptLine `json:"receipt"`
	Total    int           `json:"totalSeconds"`
}

type DraftComment struct {
	Decision string `json:"decision,omitempty"`
	Label    string `json:"label"` // Blocking, Question, Note
	// Standard says whose standard the comment rests on: "my standard",
	// "my preference", "repo convention" or "".
	Standard string `json:"standard,omitempty"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	Body     string `json:"body"`
	// Inline is false when the line is not part of the diff, in which case
	// the comment goes in the review body instead.
	Inline bool `json:"inline"`
}

type ReceiptLine struct {
	Section string `json:"section"`
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Lines   int    `json:"lines"`
	Covered string `json:"covered"`
	Seconds int    `json:"seconds"`
}

func clock(sec int) string { return fmt.Sprintf("%d:%02d", sec/60, sec%60) }

// Compose builds the review from the reviewer's answers.
func (e *Engine) Compose(key string, withReceipt bool) (*Draft, error) {
	r, ok := e.get(key)
	if !ok || r.Brief == nil {
		return nil, errors.New("this review is not ready yet")
	}
	b := r.Brief
	files := e.files(r)
	d := &Draft{}
	blocking, open, unanswered := 0, 0, 0

	for _, dec := range b.Decisions {
		a, ok := r.Answers[dec.ID]
		if !ok || a.Option == "" {
			unanswered++
			continue
		}
		var opt *model.Option
		for i := range dec.Options {
			if dec.Options[i].ID == a.Option {
				opt = &dec.Options[i]
			}
		}
		if opt == nil {
			unanswered++
			continue
		}
		body := strings.TrimSpace(a.Comment)
		if body == "" {
			body = strings.TrimSpace(opt.Comment)
		}
		label := "Note"
		switch opt.Effect {
		case model.Blocking:
			blocking++
			label = "Blocking"
		case model.Ask:
			open++
			label = "Question"
		}
		if body == "" {
			continue
		}
		c := DraftComment{Decision: dec.ID, Label: label, Path: dec.Anchor.Path, Line: dec.Anchor.Line, Body: body}
		switch {
		case dec.Rule == "repo":
			c.Standard = "repo convention"
		case dec.Rule != "":
			if rule, ok := e.Rules.Get(dec.Rule); ok && rule.Severity == "note" {
				c.Standard = "my preference"
			} else {
				c.Standard = "my standard"
			}
		}
		if f, ok := diff.Find(files, c.Path); ok && diff.HasNewLine(f, c.Line) {
			c.Inline = true
		}
		// A probe the author can rerun is worth more than the sentence.
		if opt.Effect == model.Blocking {
			for _, ev := range dec.Evidence {
				if ev.Code != "" && ev.Passed != nil && !*ev.Passed {
					c.Body += "\n\n<details><summary>Failing probe: " + ev.Title + "</summary>\n\n```\n" + ev.Code + "\n```\n\n```\n" + ev.Output + "\n```\n</details>"
					break
				}
			}
		}
		d.Comments = append(d.Comments, c)
	}
	for _, n := range r.Notes {
		if strings.TrimSpace(n.Body) == "" {
			continue
		}
		c := DraftComment{Label: "Note", Path: n.Path, Line: n.Line, Body: n.Body}
		if f, ok := diff.Find(files, n.Path); ok && diff.HasNewLine(f, n.Line) {
			c.Inline = true
		}
		d.Comments = append(d.Comments, c)
	}

	if unanswered > 0 {
		d.Locks = append(d.Locks, fmt.Sprintf("%d decision%s not answered", unanswered, plural(unanswered)))
	}
	if blocking > 0 {
		d.Locks = append(d.Locks, fmt.Sprintf("%d decision%s you marked blocking", blocking, plural(blocking)))
	}
	if open > 0 {
		d.Locks = append(d.Locks, fmt.Sprintf("%d question%s waiting on the author", open, plural(open)))
	}
	mech, audited := 0, 0
	for _, s := range b.Sections {
		if s.Kind == model.Mechanical {
			mech++
			if r.Audited[s.ID] {
				audited++
			}
		}
	}
	if mech > 0 && audited == 0 {
		d.Warnings = append(d.Warnings, fmt.Sprintf("%d mechanical section%s taken on the agent's proof alone: spot-check one hunk to keep the proof honest", mech, plural(mech)))
	}
	if r.PR.HeadSHA != r.ReadSHA {
		d.Warnings = append(d.Warnings, "The author has pushed since this was read. The review would attach to the commit that was read.")
	}
	if len(b.Uncovered) > 0 {
		d.Warnings = append(d.Warnings, "Nothing looked at: "+strings.Join(b.Uncovered, "; "))
	}

	switch {
	case blocking > 0:
		d.Event = "REQUEST_CHANGES"
	case open > 0 || unanswered > 0:
		d.Event = "COMMENT"
	default:
		d.Event = "APPROVE"
	}

	// The attention receipt: what was read by a person, and what was taken
	// on proof.
	for _, s := range b.Sections {
		sec := r.Seconds[s.ID]
		for _, dec := range b.Decisions {
			if dec.Section == s.ID {
				sec += r.Seconds[dec.ID]
			}
		}
		line := ReceiptLine{Section: s.ID, Title: s.Title, Kind: s.Kind, Lines: s.Lines, Seconds: sec}
		switch {
		case s.Kind == model.Judgment && sec > 0:
			line.Covered = "read by me"
		case s.Kind == model.Judgment:
			line.Covered = "not opened"
		case r.Audited[s.ID]:
			line.Covered = "agent's proof, spot-checked by me"
		case sec > 0:
			line.Covered = "agent's proof, skimmed by me"
		default:
			line.Covered = "agent's proof, not read by me"
		}
		d.Receipt = append(d.Receipt, line)
		d.Total += sec
	}
	d.Total += r.Seconds["context"] + r.Seconds["brief"]

	var body strings.Builder
	var folded []DraftComment
	for _, c := range d.Comments {
		if !c.Inline {
			folded = append(folded, c)
		}
	}
	for _, c := range folded {
		where := ""
		if c.Path != "" {
			where = fmt.Sprintf(" (`%s`)", c.Path)
		}
		fmt.Fprintf(&body, "**%s**%s%s: %s\n\n", c.Label, standardSuffix(c.Standard), where, c.Body)
	}
	if withReceipt && len(d.Receipt) > 0 {
		body.WriteString("<details><summary>How this was reviewed</summary>\n\n")
		body.WriteString("| Part | Changed lines | Covered by | My time |\n|---|---:|---|---:|\n")
		for _, l := range d.Receipt {
			fmt.Fprintf(&body, "| %s | %d | %s | %s |\n", strings.ReplaceAll(l.Title, "|", "/"), l.Lines, l.Covered, clock(l.Seconds))
		}
		agent := r.Agent
		if r.Model != "" && r.Model != r.Agent {
			agent += " (" + r.Model + ")"
		}
		fmt.Fprintf(&body, "\nPrepared with %s through [Docket](https://github.com/devenjarvis/docket). Claims checked: %s.\n</details>\n", agent, claimCounts(b))
	}
	d.Body = strings.TrimSpace(body.String())
	return d, nil
}

func standardSuffix(s string) string {
	if s == "" {
		return ""
	}
	return " · " + s
}

func claimCounts(b *model.Brief) string {
	n := map[string]int{}
	for _, c := range b.Claims {
		n[c.Status]++
	}
	var parts []string
	for _, k := range []string{model.Proved, model.Contradicted, model.Unproven, model.Unstated} {
		if n[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n[k], k))
		}
	}
	if len(parts) == 0 {
		return "none stated"
	}
	return strings.Join(parts, ", ")
}

// PostInput is what the reviewer signed: the verdict and the final text.
type PostInput struct {
	Event    string         `json:"event"`
	Body     string         `json:"body"`
	Comments []DraftComment `json:"comments"`
}

// Post submits the review to GitHub. Approve is refused while anything
// locks it, whatever the caller sends.
func (e *Engine) Post(ctx context.Context, key string, in PostInput) (string, error) {
	r, ok := e.get(key)
	if !ok || r.Brief == nil {
		return "", errors.New("this review is not ready yet")
	}
	draft, err := e.Compose(key, false)
	if err != nil {
		return "", err
	}
	switch in.Event {
	case "APPROVE":
		if len(draft.Locks) > 0 {
			return "", errors.New("Approve is locked: " + strings.Join(draft.Locks, ", "))
		}
	case "REQUEST_CHANGES", "COMMENT":
	default:
		return "", errors.New("unknown verdict")
	}
	files := e.files(r)
	rv := gh.ReviewInput{CommitID: r.ReadSHA, Event: in.Event, Body: strings.TrimSpace(in.Body)}
	for _, c := range in.Comments {
		body := strings.TrimSpace(c.Body)
		if body == "" {
			continue
		}
		text := "**" + c.Label + standardSuffix(c.Standard) + ".** " + body
		if f, ok := diff.Find(files, c.Path); ok && diff.HasNewLine(f, c.Line) {
			rv.Comments = append(rv.Comments, gh.ReviewComment{Path: f.Path, Line: c.Line, Side: "RIGHT", Body: text})
		} else if !strings.Contains(rv.Body, body) {
			rv.Body = strings.TrimSpace(text + "\n\n" + rv.Body)
		}
	}
	if rv.Event != "APPROVE" && rv.Body == "" && len(rv.Comments) == 0 {
		return "", errors.New("there is nothing to post: add a comment or answer a decision")
	}
	url, err := e.GH.PostReview(ctx, r.PR, rv)
	if err != nil {
		return "", err
	}

	var read []string
	for _, s := range r.Brief.Sections {
		if s.Kind == model.Judgment || r.Seconds[s.ID] > 0 || r.Audited[s.ID] {
			for _, f := range s.Files {
				read = append(read, f.Path)
			}
		}
	}
	touch(e.Home, r.PR.Owner+"/"+r.PR.Repo, Areas(read))

	e.mu.Lock()
	defer e.mu.Unlock()
	if cur := e.reviews[key]; cur != nil {
		cur.Status = "posted"
		cur.Posted = &model.Posted{Event: in.Event, URL: url, At: time.Now(), SHA: r.ReadSHA}
		e.save(cur)
	}
	return url, nil
}
