package review

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/model"
)

// Inbox bands, in the order they show. The first three are the user's to do;
// the last two are kept out of the way.
const (
	BandQuick   = "quick"   // a minute or two, and something merges or someone is unblocked
	BandCloser  = "closer"  // needs the user's judgment, or a fix only they can make
	BandSkim    = "skim"    // nothing to decide and nobody blocked on it
	BandWaiting = "waiting" // the agent, CI or another person has it
	BandDone    = "done"    // reviews the user posted
)

var bandRank = map[string]int{BandQuick: 0, BandCloser: 1, BandSkim: 2, BandWaiting: 3, BandDone: 4}

// Item is one row of the unified inbox: a review requested of the user, or
// one of their own pull requests.
type Item struct {
	Key  string   `json:"key"`
	Mine bool     `json:"mine"`
	PR   model.PR `json:"pr"`
	Band string   `json:"band"`
	// Action is what the row's button does, in a word or two.
	Action string `json:"action"`
	// Why says, in plain words, why the row sits where it does.
	Why     string `json:"why"`
	Minutes int    `json:"minutes"`
	// Since is when the wait on the user began.
	Since time.Time `json:"since"`
	// Waiter is the person waiting on the user for this, if anyone.
	Waiter string `json:"waiter,omitempty"`
	// Status says what is happening to a row in the waiting band.
	Status string `json:"status,omitempty"`
	// Live is true while the agent works on it.
	Live      bool   `json:"live,omitempty"`
	Activity  string `json:"activity,omitempty"`
	Decisions int    `json:"decisions,omitempty"`
	Answered  int    `json:"answered,omitempty"`

	// tier orders rows inside a band: 0 merges on the user's move, 1 the
	// author is reviewing one of the user's, 2 someone has waited over a
	// day, 3 the rest.
	tier int
}

// waitedLong is how long a person waits before their row moves up its band.
const waitedLong = 24 * time.Hour

// nudgeAfter is how long one of the user's pull requests sits without a
// review before Docket suggests a nudge.
const nudgeAfter = 48 * time.Hour

// BuildInbox merges the review queue and the user's own pull requests into
// one list, sorted by what to do first: the quick things that merge something
// or unblock someone, then what needs a closer look, then what only needs a
// skim. Inside a band, a row that merges on the user's move comes first, then
// one whose author is reviewing the user's own work, then anyone who has
// waited over a day, then the shortest, then the oldest.
func BuildInbox(viewer string, reviews []Summary, mine []model.Mine, now time.Time) []Item {
	// Who is reviewing the user's own pull requests, and on which one.
	reviewing := map[string]int{}
	for _, m := range mine {
		for _, login := range m.State.RequestedReviewers {
			if _, ok := reviewing[login]; !ok {
				reviewing[login] = m.PR.Number
			}
		}
		for _, r := range m.Conversation.Reviews {
			if r.Author != viewer && r.Author != m.PR.Author {
				if _, ok := reviewing[r.Author]; !ok {
					reviewing[r.Author] = m.PR.Number
				}
			}
		}
	}

	var out []Item
	for _, s := range reviews {
		out = append(out, reviewItem(s, reviewing, now))
	}
	for _, m := range mine {
		out = append(out, mineItem(viewer, m, now))
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if bandRank[a.Band] != bandRank[b.Band] {
			return bandRank[a.Band] < bandRank[b.Band]
		}
		if a.Band == BandDone {
			return a.Since.After(b.Since)
		}
		if a.tier != b.tier {
			return a.tier < b.tier
		}
		if a.Minutes != b.Minutes {
			return a.Minutes < b.Minutes
		}
		return a.Since.Before(b.Since)
	})
	return out
}

func reviewItem(s Summary, reviewing map[string]int, now time.Time) Item {
	it := Item{Key: s.Key, PR: s.PR, Since: s.Found, Waiter: s.PR.Author, Decisions: s.Decisions, Answered: s.Answered, tier: 3}
	who := s.PR.Author
	var why []string
	switch s.Lane {
	case "done":
		it.Band, it.Action, it.Waiter = BandDone, "Open", ""
		it.Why = "You posted your review."
		return it
	case "failed":
		it.Band, it.Status, it.Action, it.Waiter = BandWaiting, "Couldn't read", "Open", ""
		it.Why = firstLine(s.Error)
		if it.Why == "" {
			it.Why = "Your agent could not read it."
		}
		return it
	case "reading":
		it.Band, it.Status, it.Action, it.Waiter, it.Live = BandWaiting, "Reading", "Open", "", true
		it.Activity = s.Activity
		it.Why = "Your agent is reading it. It comes back here when it's done."
		if s.Pushed {
			it.Why = who + " pushed since your agent read it, so it's reading it again."
		}
		return it
	case "judgment":
		if s.Answered >= s.Decisions {
			it.Band, it.tier = BandQuick, 0
			if s.Reread {
				it.Action, it.Minutes = "Re-review", clamp(s.Minutes, 2, 5)
				why = append(why, who+" pushed changes and your answers still apply. Look over what changed, then post.")
			} else {
				it.Action, it.Minutes = "Post", 1
				why = append(why, "You've answered every question. It only needs posting.")
			}
		} else {
			it.Band, it.Action, it.Minutes = BandCloser, "Review", max(s.Minutes, 1)
			left := s.Decisions - s.Answered
			q := fmt.Sprintf("%s question%s for you", count(left), plural(left))
			if s.Answered > 0 {
				q += fmt.Sprintf(", and you've answered %s", strings.ToLower(count(s.Answered)))
			}
			why = append(why, q+".")
		}
	case "skim":
		it.Band, it.Action, it.Minutes = BandSkim, "Read", max(s.Minutes, 1)
		why = append(why, "No questions, but behaviour changes and a person should read it.")
	default: // clear
		it.Band, it.Action, it.Minutes = BandSkim, "Approve", max(s.Minutes, 1)
		why = append(why, "Every claim checked out. Spot-check one change and approve.")
	}
	if s.Contra > 0 {
		c := "One claim in the description didn't hold up."
		if s.Contra > 1 {
			c = fmt.Sprintf("%s claims in the description didn't hold up.", count(s.Contra))
		}
		why = append([]string{c}, why...)
	}
	if n, ok := reviewing[who]; ok {
		why = append(why, fmt.Sprintf("%s is reviewing your #%d.", who, n))
		if it.tier > 1 {
			it.tier = 1
		}
	}
	if now.Sub(it.Since) > waitedLong {
		why = append(why, fmt.Sprintf("%s has waited %s.", who, span(now.Sub(it.Since))))
		if it.tier > 2 {
			it.tier = 2
		}
	}
	it.Why = strings.Join(why, " ")
	return it
}

func mineItem(viewer string, m model.Mine, now time.Time) Item {
	p := m.PR
	it := Item{Key: p.Key(), Mine: true, PR: p, Since: p.UpdatedAt, Action: "Open", tier: 3}
	held := 0
	var heldWhy string
	fix := false
	if m.Tend != nil {
		for _, a := range m.Tend.Actions {
			if a.State != model.StateWaiting {
				continue
			}
			held++
			if a.Kind != model.ActReply {
				fix = true
			}
			if heldWhy == "" && a.Verdict != nil {
				heldWhy = a.Verdict.Reason
			}
		}
	}
	running := m.Tend != nil && m.Tend.Status == "running"
	var first *model.Blocker
	for i := range m.Blockers {
		if m.Blockers[i].OnYou {
			first = &m.Blockers[i]
			break
		}
	}
	asked := strings.Join(m.State.RequestedReviewers, ", ")

	switch {
	case held > 0:
		it.Band, it.Minutes, it.tier = BandQuick, held, 0
		it.Action = "Check reply"
		if fix {
			it.Action = "Check fix"
		}
		it.Why = fmt.Sprintf("Your agent has %s change%s waiting for your OK.", strings.ToLower(count(held)), plural(held))
		if heldWhy != "" {
			it.Why += " " + sentence(heldWhy)
		}
	case m.Lane == model.LaneReady:
		it.Band, it.Action, it.Minutes, it.tier = BandQuick, "Merge", 1, 0
		it.Why = "Approved, and every check passes."
		if m.State.AutoMerge != "" {
			it.Band, it.Status, it.Action = BandWaiting, "Merging", "Open"
			it.Why = "Auto-merge is on, so GitHub will merge it."
		}
	case running:
		it.Band, it.Status, it.Live = BandWaiting, "Agent working", true
		it.Activity = m.Tend.Activity
		it.Why = "Your agent is working on it."
		if m.Tend.Trigger != "" {
			it.Why = "Your agent is working on it: " + strings.ToLower(strings.TrimSuffix(m.Tend.Trigger, ".")) + "."
		}
	case first != nil:
		it.Why = first.Text
		switch first.Kind {
		case model.BlockDraft:
			it.Band, it.Action, it.Minutes = BandQuick, "Mark ready", 1
			it.Why = "It's still a draft."
		case model.BlockBehind:
			it.Band, it.Action, it.Minutes = BandQuick, "Update branch", 1
			it.Why = "It's behind " + p.BaseRef + ", and the repository wants it up to date."
		case model.BlockThreads:
			it.Band, it.Action, it.Minutes = BandQuick, "Reply", 2
		case model.BlockChecksFailing:
			it.Band, it.Action, it.Minutes = BandCloser, "Fix checks", 5
			failing := 0
			for _, c := range m.Checks {
				if c.Failed() {
					failing++
				}
			}
			if failing == 1 {
				it.Why = "One check is failing."
			} else if failing > 1 {
				it.Why = count(failing) + " checks are failing."
			}
		case model.BlockConflicts:
			it.Band, it.Action, it.Minutes = BandCloser, "Fix conflicts", 10
			it.Why = "It conflicts with " + p.BaseRef + "."
		case model.BlockChangesRequested:
			it.Band, it.Action, it.Minutes = BandCloser, "Respond", 5
			if who := changesBy(viewer, m.Conversation); who != "" {
				it.Waiter = who
				it.Why = who + " asked for changes."
			}
		default:
			it.Band, it.Minutes = BandCloser, 5
		}
	default:
		it.Band = BandWaiting
		var b *model.Blocker
		if len(m.Blockers) > 0 {
			b = &m.Blockers[0]
		}
		switch {
		case b != nil && b.Kind == model.BlockChecksPending:
			it.Status, it.Why = "Checks running", b.Text
		case b != nil && b.Kind == model.BlockReviewRequired:
			it.Status, it.Why = "Waiting for review", "Needs an approving review."
			if asked != "" {
				it.Why = "Asked " + asked + "."
			}
			if now.Sub(p.UpdatedAt) > nudgeAfter {
				it.Band, it.Status, it.Action, it.Minutes = BandQuick, "", "Nudge", 1
				it.Why = fmt.Sprintf("Nothing has happened on it for %s.", span(now.Sub(p.UpdatedAt)))
				if asked != "" {
					it.Why += " You asked " + asked + "; ping them or ask someone else."
				} else {
					it.Why += " Ask someone to review it."
				}
			}
		case b != nil:
			it.Status, it.Why = "Waiting", b.Text
		default:
			it.Status, it.Why = "Waiting", "GitHub hasn't said what it's waiting on yet."
		}
	}
	return it
}

// changesBy is the latest reviewer who asked for changes.
func changesBy(viewer string, c model.Conversation) string {
	who := ""
	var at time.Time
	for _, r := range c.Reviews {
		if r.State == "CHANGES_REQUESTED" && r.Author != viewer && !r.At.Before(at) {
			who, at = r.Author, r.At
		}
	}
	return who
}

// span says how long, in the largest whole unit: "3 hours", "2 days".
func span(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	case d >= 24*time.Hour:
		return "a day"
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	case d >= time.Hour:
		return "an hour"
	default:
		return fmt.Sprintf("%d minutes", max(int(d.Minutes()), 1))
	}
}

// count writes small numbers as words, to open a sentence.
func count(n int) string {
	words := []string{"No", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return fmt.Sprint(n)
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}

// sentence makes s end like one.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasSuffix(s, ".") || strings.HasSuffix(s, "?") || strings.HasSuffix(s, "!") {
		return s
	}
	return s + "."
}

func clamp(n, lo, hi int) int { return min(max(n, lo), hi) }
