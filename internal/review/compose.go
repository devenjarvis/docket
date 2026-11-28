package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/diff"
	"github.com/devenjarvis/docket/internal/gh"
	"github.com/devenjarvis/docket/internal/ledger"
	"github.com/devenjarvis/docket/internal/model"
)

// Draft is the review as it would be posted, plus why Approve is or is not
// available.
type Draft struct {
	// Event is the verdict Docket suggests: REQUEST_CHANGES when anything is
	// blocking, COMMENT when a question is open, APPROVE when neither.
	Event string `json:"event"`
	// Body is the review summary: the reviewer's own words, empty to start.
	// Comments that cannot sit on a line are folded in when it posts, and
	// so is the receipt when the reviewer includes it.
	Body     string         `json:"body"`
	Comments []DraftComment `json:"comments"`
	// Locks lists what stands between the reviewer and Approve. Empty means
	// Approve is open.
	Locks []Reason `json:"locks"`
	// Warnings are things worth knowing that do not lock anything.
	Warnings []Reason      `json:"warnings"`
	Receipt  []ReceiptLine `json:"receipt"`
	// ReceiptMarkdown is the receipt as it would post, and IncludeReceipt
	// whether the reviewer's setting puts it in by default.
	ReceiptMarkdown string `json:"receiptMarkdown"`
	IncludeReceipt  bool   `json:"includeReceipt"`
	Total           int    `json:"totalSeconds"`
}

// Reason is one lock or warning, with where to act on it: a decision to
// answer or a section to spot-check.
type Reason struct {
	Text     string `json:"text"`
	Decision string `json:"decision,omitempty"`
	Section  string `json:"section,omitempty"`
}

func reasonTexts(rs []Reason) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Text
	}
	return out
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
	d := &Draft{IncludeReceipt: withReceipt}
	blocking, open, unanswered := 0, 0, 0
	// The first decision behind each lock, so the lock can lead to it.
	var firstOpen, firstBlocking, firstAsk string
	mark := func(at *string, id string) {
		if *at == "" {
			*at = id
		}
	}

	for _, dec := range b.Decisions {
		a, ok := r.Answers[dec.ID]
		if !ok || a.Option == "" {
			unanswered++
			mark(&firstOpen, dec.ID)
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
			mark(&firstOpen, dec.ID)
			continue
		}
		body := strings.TrimSpace(a.Comment)
		if body == "" {
			body = strings.TrimSpace(opt.Comment)
		}
		if r.Self {
			// On a self-review only the notes for reviewers post. A fix is
			// the agent's or the author's to make, not a comment, and an
			// accepted finding needs nothing.
			if opt.Effect == model.Blocking {
				blocking++
				mark(&firstBlocking, dec.ID)
			}
			if opt.Effect != model.Ask || body == "" {
				continue
			}
			c := DraftComment{Decision: dec.ID, Label: "Note", Path: dec.Anchor.Path, Line: dec.Anchor.Line, Body: body}
			if f, ok := diff.Find(files, c.Path); ok && diff.HasNewLine(f, c.Line) {
				c.Inline = true
			}
			d.Comments = append(d.Comments, c)
			continue
		}
		label := "Note"
		switch opt.Effect {
		case model.Blocking:
			blocking++
			mark(&firstBlocking, dec.ID)
			label = "Blocking"
		case model.Ask:
			open++
			mark(&firstAsk, dec.ID)
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

	if r.Self {
		return e.composeSelf(r, d, unanswered, firstOpen, blocking, firstBlocking), nil
	}
	if unanswered > 0 {
		d.Locks = append(d.Locks, Reason{Text: fmt.Sprintf("%d decision%s not answered", unanswered, plural(unanswered)), Decision: firstOpen})
	}
	if blocking > 0 {
		d.Locks = append(d.Locks, Reason{Text: fmt.Sprintf("%d decision%s you marked blocking", blocking, plural(blocking)), Decision: firstBlocking})
	}
	if open > 0 {
		d.Locks = append(d.Locks, Reason{Text: fmt.Sprintf("%d question%s waiting on the author", open, plural(open)), Decision: firstAsk})
	}
	mech, audited, firstMech := 0, 0, ""
	for _, s := range b.Sections {
		if s.Kind == model.Mechanical {
			mech++
			mark(&firstMech, s.ID)
			if r.Audited[s.ID] {
				audited++
			}
		}
	}
	if mech > 0 && audited == 0 {
		d.Warnings = append(d.Warnings, Reason{Text: fmt.Sprintf("%d mechanical section%s taken on the agent's proof alone: spot-check one hunk to keep the proof honest", mech, plural(mech)), Section: firstMech})
	}
	if r.PR.HeadSHA != r.ReadSHA {
		d.Warnings = append(d.Warnings, Reason{Text: "The author has pushed since this was read. The review would attach to the commit that was read."})
	}
	if len(b.Uncovered) > 0 {
		d.Warnings = append(d.Warnings, Reason{Text: "Nothing looked at: " + strings.Join(b.Uncovered, "; ")})
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

	// The summary starts empty: it is for the reviewer's own words. Comments
	// that cannot sit on a line are folded in when the review posts.
	if len(d.Receipt) > 0 {
		var body strings.Builder
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
		d.ReceiptMarkdown = strings.TrimSpace(body.String())
	}
	return d, nil
}

// composeSelf finishes a self-review's draft: it posts only as a comment,
// with the author's notes for reviewers, and nothing locks it. What is
// unsettled or still to fix is said as a warning.
func (e *Engine) composeSelf(r *model.Review, d *Draft, unanswered int, firstOpen string, fixes int, firstFix string) *Draft {
	d.Event, d.IncludeReceipt = "COMMENT", false
	if unanswered > 0 {
		d.Warnings = append(d.Warnings, Reason{Text: fmt.Sprintf("%d finding%s not settled yet", unanswered, plural(unanswered)), Decision: firstOpen})
	}
	if fixes > 0 && (r.Handed == nil || r.Handed.SHA != r.ReadSHA) {
		d.Warnings = append(d.Warnings, Reason{Text: fmt.Sprintf("%d finding%s you chose to fix: hand %s to the agent, or fix %s yourself", fixes, plural(fixes), them(fixes), them(fixes)), Decision: firstFix})
	}
	if r.PR.HeadSHA != r.ReadSHA {
		d.Warnings = append(d.Warnings, Reason{Text: "You pushed since this was read. Notes would attach to the commit that was read."})
	}
	return d
}

func them(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
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
	// Receipt puts the attention receipt at the end of the review body,
	// composed from the time and spot-checks as they stand at posting.
	Receipt bool `json:"receipt"`
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
	if r.Self && in.Event != "COMMENT" {
		return "", errors.New("your own pull request takes notes for reviewers, not a verdict")
	}
	switch in.Event {
	case "APPROVE":
		if len(draft.Locks) > 0 {
			return "", errors.New("Approve is locked: " + strings.Join(reasonTexts(draft.Locks), ", "))
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
		if r.Self {
			// The author's own note reads as theirs, with no label.
			text = body
		}
		if f, ok := diff.Find(files, c.Path); ok && diff.HasNewLine(f, c.Line) {
			rv.Comments = append(rv.Comments, gh.ReviewComment{Path: f.Path, Line: c.Line, Side: "RIGHT", Body: text})
		} else if !strings.Contains(rv.Body, body) {
			rv.Body = strings.TrimSpace(text + "\n\n" + rv.Body)
		}
	}
	if rv.Event != "APPROVE" && rv.Body == "" && len(rv.Comments) == 0 {
		return "", errors.New("there is nothing to post: add a comment or answer a decision")
	}
	if in.Receipt && draft.ReceiptMarkdown != "" && !r.Self {
		rv.Body = strings.TrimSpace(rv.Body + "\n\n" + draft.ReceiptMarkdown)
	}
	url, err := e.GH.PostReview(ctx, r.PR, rv)
	if err != nil {
		return "", err
	}
	if r.Self {
		e.mu.Lock()
		defer e.mu.Unlock()
		if cur := e.reviews[key]; cur != nil {
			cur.Posted = &model.Posted{Event: in.Event, URL: url, At: time.Now(), SHA: r.ReadSHA}
			e.save(cur)
		}
		return url, nil
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
	e.record(postEntry(*r, in.Event, in.Receipt))

	e.mu.Lock()
	defer e.mu.Unlock()
	if cur := e.reviews[key]; cur != nil {
		cur.Status = "posted"
		cur.Posted = &model.Posted{Event: in.Event, URL: url, At: time.Now(), SHA: r.ReadSHA}
		e.save(cur)
	}
	return url, nil
}

// postEntry is the ledger line for a review the user posted.
func postEntry(r model.Review, event string, receipt bool) ledger.Entry {
	sum := summarize(&r)
	en := ledger.Entry{Kind: ledger.KindPost, Repo: r.PR.Owner + "/" + r.PR.Repo, Number: r.PR.Number, Event: event,
		Agent: r.Agent, Model: r.Model, NeedLines: sum.NeedLines, TotalLines: sum.TotalLines, Decisions: sum.Decisions,
		Answered: sum.Answered, Asks: len(r.Asks), Receipt: receipt, Time: map[string]int{}, Strategy: r.Strategy}
	decisions, sections := map[string]model.Decision{}, map[string]bool{}
	if b := r.Brief; b != nil {
		for _, d := range b.Decisions {
			decisions[d.ID] = d
		}
		for _, s := range b.Sections {
			sections[s.ID] = true
			if s.Kind == model.Mechanical {
				en.Mechanical++
				if r.Audited[s.ID] {
					en.Audited++
				}
			}
		}
	}
	for id, sec := range r.Seconds {
		en.YourSeconds += sec
		switch {
		case id == "brief" || id == "context":
			en.Time[id] += sec
		case decisions[id].ID != "":
			en.Time["decisions"] += sec
		case sections[id]:
			en.Time["diff"] += sec
		}
	}
	for id, a := range r.Answers {
		d, ok := decisions[id]
		if !ok || a.Option == "" {
			continue
		}
		if a.Comment != "" {
			en.Edited++
		}
		for _, o := range d.Options {
			if o.ID == a.Option && o.Effect == model.Blocking {
				en.Blocking++
				if d.Lens != "" {
					if en.LensBlocking == nil {
						en.LensBlocking = map[string]int{}
					}
					en.LensBlocking[d.Lens]++
				}
			}
		}
	}
	for _, d := range decisions {
		if d.Lens != "" {
			if en.LensDecisions == nil {
				en.LensDecisions = map[string]int{}
			}
			en.LensDecisions[d.Lens]++
		}
	}
	return en
}
