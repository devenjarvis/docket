package review

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/model"
)

// Self-review modes, from Settings.
const (
	SelfAuto = "auto" // every new head of the user's own pull requests
	SelfAsk  = "ask"  // only when the user asks
	SelfOff  = "off"
)

// selfMode is the self-review mode for a repository. With no SelfMode set,
// self-review is off: the engine was built without it.
func (e *Engine) selfMode(repo string) string {
	if e.SelfMode == nil {
		return SelfOff
	}
	switch m := e.SelfMode(repo); m {
	case SelfAsk, SelfOff:
		return m
	}
	return SelfAuto
}

// syncSelfLocked keeps the self-review of one of the user's own pull
// requests in step with it: one is started when Docket meets the pull
// request and on every new head, in auto mode. The caller holds e.mu.
func (e *Engine) syncSelfLocked(pr model.PR) {
	key := pr.Key()
	mode := e.selfMode(pr.Owner + "/" + pr.Repo)
	r := e.reviews[key]
	if r != nil && !r.Self {
		// Added by hand before Docket knew it was the user's own.
		r.Self = true
	}
	if mode == SelfOff {
		if r != nil {
			e.dropSelfLocked(key)
		}
		return
	}
	pr.Why = []string{"yours"}
	if r == nil {
		if mode != SelfAuto {
			return
		}
		r = &model.Review{PR: pr, Self: true, Status: "queued", FoundAt: time.Now()}
		fix(r)
		e.reviews[key] = r
		e.save(r)
		e.enqueueLocked(key)
		return
	}
	newHead := r.PR.HeadSHA != pr.HeadSHA
	changed := !reflect.DeepEqual(r.PR, pr)
	r.PR = pr
	busy := r.Status == "queued" || r.Status == "reading"
	// A failed read is tried again only on a new head; the user can retry it
	// by hand.
	if mode == SelfAuto && r.ReadSHA != pr.HeadSHA && (newHead || (!busy && r.Status != "failed")) {
		if c := e.cancels[key]; c != nil {
			c()
		}
		r.Status, r.Error, r.Activity, r.Posted = "queued", "", "", nil
		e.enqueueLocked(key)
		changed = true
	}
	if changed {
		e.save(r)
	}
}

// dropSelfLocked removes a self-review. The caller holds e.mu.
func (e *Engine) dropSelfLocked(key string) {
	r := e.reviews[key]
	if r == nil || !r.Self {
		return
	}
	if c := e.cancels[key]; c != nil {
		c()
	}
	delete(e.reviews, key)
	_ = os.Remove(e.statePath(r.PR))
}

// SelfReview starts a self-review of one of the user's own pull requests
// because they asked, or reads it again.
func (e *Engine) SelfReview(key string) error {
	e.mu.Lock()
	e.loadMineLocked()
	m := e.mine[key]
	e.mu.Unlock()
	if m == nil {
		return errors.New("no such pull request of yours")
	}
	return e.selfReview(m.PR)
}

func (e *Engine) selfReview(pr model.PR) error {
	repoName := pr.Owner + "/" + pr.Repo
	if e.off(repoName) {
		return errors.New("Docket is turned off for this repository: change it in Settings")
	}
	if e.selfMode(repoName) == SelfOff {
		return errors.New("self-review is off for this repository: change it in Settings, under Policy")
	}
	key := pr.Key()
	pr.Why = []string{"yours"}
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.reviews[key]
	if r == nil {
		r = &model.Review{PR: pr, Self: true, FoundAt: time.Now()}
		fix(r)
		e.reviews[key] = r
	}
	r.Self, r.PR = true, pr
	if r.Status == "queued" || r.Status == "reading" {
		return nil
	}
	if c := e.cancels[key]; c != nil {
		c()
	}
	r.Status, r.Error, r.Activity = "queued", "", ""
	e.save(r)
	e.enqueueLocked(key)
	return nil
}

// selfSummaries is every self-review's queue row, by key.
func (e *Engine) selfSummaries() map[string]Summary {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[string]Summary{}
	for k, r := range e.reviews {
		if r.Self {
			out[k] = summarize(r)
		}
	}
	return out
}

// fixTasks lists the findings the author chose to fix, as the agent will
// get them.
func fixTasks(r *model.Review) []model.TendTask {
	if r.Brief == nil {
		return nil
	}
	var out []model.TendTask
	for _, d := range r.Brief.Decisions {
		a, ok := r.Answers[d.ID]
		if !ok {
			continue
		}
		for _, o := range d.Options {
			if o.ID != a.Option || o.Effect != model.Blocking {
				continue
			}
			ask := strings.TrimSpace(a.Comment)
			if ask == "" {
				ask = strings.TrimSpace(o.Comment)
			}
			if ask == "" {
				ask = o.Label
			}
			out = append(out, model.TendTask{ID: d.ID, Title: d.Title, Detail: d.Detail, Path: d.Anchor.Path, Line: d.Anchor.Line, Ask: ask})
		}
	}
	return out
}

// HandOff settles the findings the author chose to fix: by "agent", a run
// on the branch that makes them, through the same gate as any other step;
// by "you", a note that the author makes them by hand.
func (e *Engine) HandOff(key, by string) error {
	if by != "agent" && by != "you" {
		return errors.New("hand the fixes to the agent or to yourself")
	}
	r, ok := e.get(key)
	if !ok || !r.Self || r.Brief == nil {
		return errors.New("this self-review is not ready yet")
	}
	tasks := fixTasks(r)
	if len(tasks) == 0 {
		return errors.New("no finding is marked to fix")
	}
	if by == "agent" {
		if err := e.startTend(key, nil, tasks, false); err != nil {
			return err
		}
	}
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if cur := e.reviews[key]; cur != nil {
		cur.Handed = &model.Handed{By: by, Decisions: ids, SHA: cur.ReadSHA, At: time.Now()}
		e.save(cur)
	}
	return nil
}
