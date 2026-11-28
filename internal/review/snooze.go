package review

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/model"
)

// Snooze kinds.
const (
	SnoozeChange   = "change"   // until the pull request has new commits or activity
	SnoozeTomorrow = "tomorrow" // until 8 in the morning, the next day
)

// Snooze is one pull request the user put aside.
type Snooze struct {
	Kind string `json:"kind"`
	// Until is when a "tomorrow" snooze ends.
	Until time.Time `json:"until,omitempty"`
	// Head and Updated are the pull request as it was, for a "change"
	// snooze: it ends when either moves.
	Head    string    `json:"head,omitempty"`
	Updated time.Time `json:"updated,omitempty"`
	At      time.Time `json:"at"`
}

// Awake says whether the snooze is over for pr at now.
func (s Snooze) Awake(pr model.PR, now time.Time) bool {
	switch s.Kind {
	case SnoozeTomorrow:
		return !now.Before(s.Until)
	case SnoozeChange:
		return (s.Head != "" && pr.HeadSHA != s.Head) || pr.UpdatedAt.After(s.Updated)
	}
	return true
}

// tomorrowMorning is 8 in the morning on the day after now, in now's zone.
func tomorrowMorning(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d+1, 8, 0, 0, 0, now.Location())
}

func (e *Engine) snoozePath() string { return filepath.Join(e.Home, "snoozed.json") }

// loadSnoozesLocked reads snoozed.json once. The caller holds e.mu.
func (e *Engine) loadSnoozesLocked() {
	if e.snoozes != nil {
		return
	}
	e.snoozes = map[string]Snooze{}
	b, err := os.ReadFile(e.snoozePath())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			e.Logf("could not read snoozes: %v", err)
		}
		return
	}
	if err := json.Unmarshal(b, &e.snoozes); err != nil {
		e.Logf("could not read snoozes: %v", err)
		e.snoozes = map[string]Snooze{}
	}
}

func (e *Engine) saveSnoozesLocked() {
	if err := config.WriteJSON(e.snoozePath(), e.snoozes); err != nil {
		e.Logf("could not save snoozes: %v", err)
	}
}

// SnoozeKey puts a pull request aside until it changes or until tomorrow
// morning. An empty kind wakes it now.
func (e *Engine) SnoozeKey(key, kind string, now time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.loadSnoozesLocked()
	if kind == "" {
		delete(e.snoozes, key)
		e.saveSnoozesLocked()
		e.Hub.Send(Event{Type: "queue"})
		return nil
	}
	var pr model.PR
	if r := e.reviews[key]; r != nil {
		pr = r.PR
	} else {
		e.loadMineLocked()
		m := e.mine[key]
		if m == nil {
			return errors.New("no such pull request in the inbox")
		}
		pr = m.PR
	}
	s := Snooze{Kind: kind, At: now}
	switch kind {
	case SnoozeChange:
		s.Head, s.Updated = pr.HeadSHA, pr.UpdatedAt
	case SnoozeTomorrow:
		s.Until = tomorrowMorning(now)
	default:
		return errors.New("snooze until it changes or until tomorrow")
	}
	e.snoozes[key] = s
	e.saveSnoozesLocked()
	e.Hub.Send(Event{Type: "queue"})
	return nil
}

// Unified is the unified inbox as it stands now. Snoozes that are over, or
// whose pull request left the inbox, are dropped here.
func (e *Engine) Unified(now time.Time) []Item {
	st, mine := e.List(), e.Mine()
	e.mu.Lock()
	e.loadSnoozesLocked()
	prs := map[string]model.PR{}
	for _, r := range st.Reviews {
		prs[r.Key] = r.PR
	}
	for _, m := range mine {
		prs[m.PR.Key()] = m.PR
	}
	snoozes := map[string]Snooze{}
	changed := false
	for k, s := range e.snoozes {
		pr, ok := prs[k]
		if !ok || s.Awake(pr, now) {
			delete(e.snoozes, k)
			changed = true
			continue
		}
		snoozes[k] = s
	}
	if changed {
		e.saveSnoozesLocked()
	}
	e.mu.Unlock()
	return BuildInbox(st.Viewer, st.Reviews, mine, e.selfSummaries(), snoozes, now)
}
