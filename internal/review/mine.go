package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/model"
)

func (e *Engine) mineDir() string { return filepath.Join(e.Home, "state", "mine") }

func (e *Engine) minePath(pr model.PR) string {
	return filepath.Join(e.mineDir(), fmt.Sprintf("%s__%s__%d.json", pr.Owner, pr.Repo, pr.Number))
}

// loadMineLocked reads the own pull requests saved by an earlier run, once.
// The caller holds e.mu.
func (e *Engine) loadMineLocked() {
	if e.mine != nil {
		return
	}
	e.mine = map[string]*model.Mine{}
	entries, _ := os.ReadDir(e.mineDir())
	for _, en := range entries {
		if !strings.HasSuffix(en.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(e.mineDir(), en.Name()))
		if err != nil {
			continue
		}
		var m model.Mine
		if json.Unmarshal(b, &m) == nil && m.PR.Number != 0 {
			if t := m.Tend; t != nil {
				// Docket stopped during the run or a step; nothing carries
				// them on.
				if t.Status == "running" {
					t.Status, t.Activity, t.Error = "failed", "", "Docket stopped before this run finished."
				}
				for _, list := range [][]model.TendAction{t.Actions, t.Earlier} {
					for i := range list {
						if list[i].State == model.StateWorking {
							list[i].State, list[i].Result = model.StateFailed, "Docket stopped while doing this; check the pull request before trying again."
						}
					}
				}
			}
			e.mine[m.PR.Key()] = &m
		}
	}
}

// syncMine looks at every open pull request the user wrote. The detail is
// read on every look, because a check finishing does not change the pull
// request's updated time.
func (e *Engine) syncMine(ctx context.Context) error {
	if e.Inbox == nil {
		return nil
	}
	list, err := e.Inbox.Mine(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.loadMineLocked()
	first := !e.mineSeen
	e.mu.Unlock()

	open := map[string]bool{}
	for _, found := range list {
		key := found.Key()
		open[key] = true
		if err := e.refreshMine(ctx, found, !first); err != nil {
			e.Logf("could not load %s: %v", key, err)
		}
	}
	e.mu.Lock()
	for key, m := range e.mine {
		if !open[key] {
			delete(e.mine, key)
			_ = os.Remove(e.minePath(m.PR))
		}
	}
	e.mineSeen = true
	e.mu.Unlock()
	e.Hub.Send(Event{Type: "mine"})
	return nil
}

// refreshMine reads one own pull request again. found carries at least the
// key and the updated time; the full pull request is read only when that
// time moved.
func (e *Engine) refreshMine(ctx context.Context, found model.PR, notify bool) error {
	key := found.Key()
	e.mu.Lock()
	e.loadMineLocked()
	var prev *model.Mine
	if cur := e.mine[key]; cur != nil {
		cp := clone(cur)
		prev = &cp
	}
	e.mu.Unlock()

	pr := found
	if prev != nil && prev.PR.HeadSHA != "" && prev.PR.UpdatedAt.Equal(found.UpdatedAt) {
		pr = prev.PR
	} else {
		full, err := e.GH.PR(ctx, found.Owner, found.Repo, found.Number)
		if err != nil {
			return err
		}
		full.Why = []string{"yours"}
		pr = full
	}
	st, checks, conv, err := e.Inbox.Detail(ctx, pr.Owner, pr.Repo, pr.Number)
	if err != nil {
		return err
	}
	viewer := e.Viewer()
	bs := model.Blockers(viewer, pr.Draft, st, checks, conv)
	next := &model.Mine{PR: pr, State: st, Checks: checks, Conversation: conv, Blockers: bs, Lane: model.LaneOf(bs), FetchedAt: time.Now()}

	e.mu.Lock()
	// The run and what was seen come from the latest state, not prev: a
	// run may have changed them while the detail was being read.
	prevSeen := ""
	if p := e.mine[key]; p != nil {
		prevSeen = p.Seen
	}
	tendNow := e.maybeTendLocked(viewer, e.mine[key], next)
	e.mine[key] = next
	if err := config.WriteJSON(e.minePath(pr), next); err != nil {
		e.Logf("could not save %s: %v", key, err)
	}
	e.mu.Unlock()
	e.Hub.Send(Event{Type: "mine", Key: key})
	if tendNow {
		if err := e.startTend(key, nil, true); err != nil {
			e.Logf("%s: could not start the agent: %v", key, err)
			// Nothing covered the new items, so they stay unseen.
			e.mu.Lock()
			if m := e.mine[key]; m != nil {
				m.Seen = prevSeen
			}
			e.mu.Unlock()
		}
	}

	if notify && e.Notify != nil {
		if body := news(viewer, prev, next); body != "" {
			e.Notify(fmt.Sprintf("%s/%s #%d", pr.Owner, pr.Repo, pr.Number), body, key)
		}
	}
	return nil
}

// news says what changed on an own pull request that the user would want to
// hear about: a lane it moved into that wants them, or something someone
// else said. A pull request seen for the first time is not news.
func news(viewer string, prev, next *model.Mine) string {
	if prev == nil {
		return ""
	}
	var parts []string
	if next.Lane != prev.Lane {
		switch next.Lane {
		case model.LaneReady:
			parts = append(parts, "It is ready to merge.")
		case model.LaneNeedsYou:
			why := ""
			for _, b := range next.Blockers {
				if b.OnYou {
					why = " " + b.Text
					break
				}
			}
			parts = append(parts, "It needs you."+why)
		}
	}
	seen := map[string]bool{}
	for _, r := range prev.Conversation.Reviews {
		seen[r.ID] = true
	}
	for _, t := range prev.Conversation.Threads {
		for _, c := range t.Comments {
			seen[c.ID] = true
		}
	}
	for _, c := range prev.Conversation.Comments {
		seen[c.ID] = true
	}
	who := map[string]bool{}
	var order []string
	say := func(s string) {
		if !who[s] {
			who[s] = true
			order = append(order, s)
		}
	}
	for _, r := range next.Conversation.Reviews {
		if !seen[r.ID] && r.Author != viewer {
			switch r.State {
			case "APPROVED":
				say(r.Author + " approved")
			case "CHANGES_REQUESTED":
				say(r.Author + " requested changes")
			default:
				say(r.Author + " reviewed")
			}
		}
	}
	for _, t := range next.Conversation.Threads {
		for _, c := range t.Comments {
			if !seen[c.ID] && c.Author != viewer {
				say(c.Author + " commented")
			}
		}
	}
	for _, c := range next.Conversation.Comments {
		if !seen[c.ID] && c.Author != viewer {
			say(c.Author + " commented")
		}
	}
	if len(order) > 0 {
		parts = append(parts, strings.Join(order, ", ")+".")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

var laneRank = map[string]int{model.LaneNeedsYou: 0, model.LaneReady: 1, model.LaneWaiting: 2}

// Mine returns the user's own open pull requests, the ones that want them
// first.
func (e *Engine) Mine() []model.Mine {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.loadMineLocked()
	out := make([]model.Mine, 0, len(e.mine))
	for _, m := range e.mine {
		out = append(out, clone(m))
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if laneRank[a.Lane] != laneRank[b.Lane] {
			return laneRank[a.Lane] < laneRank[b.Lane]
		}
		return a.PR.UpdatedAt.After(b.PR.UpdatedAt)
	})
	return out
}

// MineOne returns one own pull request.
func (e *Engine) MineOne(key string) (model.Mine, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.loadMineLocked()
	m, ok := e.mine[key]
	if !ok {
		return model.Mine{}, false
	}
	return clone(m), true
}

// clone copies an own pull request deeply enough that a run changing it
// later does not change the copy. The caller holds e.mu.
func clone(m *model.Mine) model.Mine {
	cp := *m
	if m.Tend != nil {
		t := *m.Tend
		t.Actions = append([]model.TendAction(nil), m.Tend.Actions...)
		t.Notes = append([]string(nil), m.Tend.Notes...)
		t.Earlier = append([]model.TendAction(nil), m.Tend.Earlier...)
		cp.Tend = &t
	}
	return cp
}

// Action is something the user does to a pull request from Docket.
type Action struct {
	// Kind is reply, resolve, unresolve, comment, merge, auto-merge,
	// update-branch, ready, request-review or rerun.
	Kind   string   `json:"kind"`
	Thread string   `json:"thread,omitempty"`
	Body   string   `json:"body,omitempty"`
	Method string   `json:"method,omitempty"`
	On     bool     `json:"on,omitempty"`
	Logins []string `json:"logins,omitempty"`
	Run    int64    `json:"run,omitempty"`
}

// ownOnly lists the actions that only the author's own pull requests take.
var ownOnly = map[string]bool{"merge": true, "auto-merge": true, "update-branch": true, "ready": true, "request-review": true, "rerun": true}

// prFor finds a pull request Docket knows, own or under review.
func (e *Engine) prFor(key string) (model.PR, *model.Mine, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.loadMineLocked()
	if m := e.mine[key]; m != nil {
		cp := clone(m)
		return m.PR, &cp, true
	}
	if r := e.reviews[key]; r != nil {
		return r.PR, nil, true
	}
	return model.PR{}, nil, false
}

// Act does one thing to a pull request through GitHub and reads it again.
func (e *Engine) Act(ctx context.Context, key string, a Action) error {
	if e.Inbox == nil {
		return errors.New("this build cannot act on pull requests")
	}
	pr, m, ok := e.prFor(key)
	if !ok {
		return errors.New("no such pull request")
	}
	if ownOnly[a.Kind] && m == nil {
		return errors.New("only your own pull requests can do that")
	}
	body := strings.TrimSpace(a.Body)
	var err error
	switch a.Kind {
	case "reply":
		if body == "" || a.Thread == "" {
			return errors.New("write a reply first")
		}
		_, err = e.Inbox.Reply(ctx, pr, a.Thread, body)
	case "resolve", "unresolve":
		if a.Thread == "" {
			return errors.New("no thread named")
		}
		err = e.Inbox.Resolve(ctx, pr, a.Thread, a.Kind == "resolve")
	case "comment":
		if body == "" {
			return errors.New("write a comment first")
		}
		err = e.Inbox.Comment(ctx, pr, body)
	case "merge":
		method := strings.ToUpper(a.Method)
		if !contains(m.State.Methods, method) {
			return fmt.Errorf("this repository does not allow %s merges", strings.ToLower(a.Method))
		}
		err = e.Inbox.Merge(ctx, pr, method, pr.HeadSHA)
	case "auto-merge":
		method := strings.ToUpper(a.Method)
		if a.On && !contains(m.State.Methods, method) {
			return fmt.Errorf("this repository does not allow %s merges", strings.ToLower(a.Method))
		}
		err = e.Inbox.AutoMerge(ctx, pr, m.State.NodeID, method, a.On)
	case "update-branch":
		err = e.Inbox.UpdateBranch(ctx, pr)
	case "ready":
		err = e.Inbox.MarkReady(ctx, pr, m.State.NodeID)
	case "request-review":
		err = e.Inbox.RequestReview(ctx, pr, a.Logins)
	case "rerun":
		err = e.Inbox.Rerun(ctx, pr, a.Run)
	default:
		return fmt.Errorf("unknown action %q", a.Kind)
	}
	if err != nil {
		return err
	}
	if m == nil {
		e.Hub.Send(Event{Type: "conversation", Key: key})
		return nil
	}
	if a.Kind == "merge" {
		e.mu.Lock()
		delete(e.mine, key)
		_ = os.Remove(e.minePath(pr))
		e.mu.Unlock()
		e.Hub.Send(Event{Type: "mine"})
		return nil
	}
	// The detail is read again at once, so the page shows what the user
	// just did. Their own action is not news.
	found := pr
	found.UpdatedAt = time.Time{}
	return e.refreshMine(ctx, found, false)
}

// Conversation reads what people have said on any pull request Docket knows.
func (e *Engine) Conversation(ctx context.Context, key string) (model.Conversation, error) {
	if e.Inbox == nil {
		return model.Conversation{}, errors.New("this build cannot read conversations")
	}
	pr, m, ok := e.prFor(key)
	if !ok {
		return model.Conversation{}, errors.New("no such pull request")
	}
	if m != nil && time.Since(m.FetchedAt) < 10*time.Second {
		return m.Conversation, nil
	}
	_, _, conv, err := e.Inbox.Detail(ctx, pr.Owner, pr.Repo, pr.Number)
	return conv, err
}

// FailedLog returns the end of a failed CI job's log on an own pull request.
func (e *Engine) FailedLog(ctx context.Context, key string, job int64) (string, error) {
	if e.Inbox == nil {
		return "", errors.New("this build cannot read CI logs")
	}
	pr, m, ok := e.prFor(key)
	if !ok || m == nil {
		return "", errors.New("no such pull request of yours")
	}
	for _, c := range m.Checks {
		if c.JobID == job && job != 0 {
			return e.Inbox.FailedLog(ctx, pr, job)
		}
	}
	return "", errors.New("no such job on this pull request")
}
