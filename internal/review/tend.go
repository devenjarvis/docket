package review

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/acp"
	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/repo"
	"github.com/devenjarvis/docket/internal/tend"
)

// tendTimeout bounds one tend run, the agent's work and the gate together.
const tendTimeout = 30 * time.Minute

// maxAutoRuns is how many runs Docket starts on its own, one after another,
// before it waits for the user to do something on that pull request. It
// stops an agent whose fix keeps failing CI from trying forever.
const maxAutoRuns = 3

// maxEarlier is how many steps from earlier runs a pull request keeps.
const maxEarlier = 60

// errBranchMoved is a push that found the branch moved on GitHub.
var errBranchMoved = errors.New("someone pushed to the branch meanwhile, so nothing was pushed")

// Tend modes, from Settings.
const (
	TendAuto = "auto"
	TendAsk  = "ask"
	TendOff  = "off"
)

func (e *Engine) tendMode() string {
	if e.TendMode == nil {
		return TendAuto
	}
	switch m := e.TendMode(); m {
	case TendAsk, TendOff:
		return m
	}
	return TendAuto
}

// triggers lists what on an own pull request the agent can act on: a thread
// where someone else spoke last, a failed check, a conflict. Each item names
// its exact state, so a new comment or a new failure is a new item.
func triggers(viewer string, m *model.Mine) []string {
	var out []string
	for _, t := range m.Conversation.Threads {
		if !t.Resolved && len(t.Comments) > 0 && t.Last().Author != viewer {
			out = append(out, "thread:"+t.ID+":"+t.Last().ID)
		}
	}
	for _, c := range m.Checks {
		if c.Failed() {
			out = append(out, "check:"+c.ID)
		}
	}
	if m.State.Mergeable == "CONFLICTING" || m.State.MergeStateStatus == "DIRTY" {
		out = append(out, "conflicts:"+m.PR.HeadSHA)
	}
	sort.Strings(out)
	return out
}

// maybeTendLocked decides whether next, just read, needs a run, and records
// what it saw. prev is what was known before; nil means Docket just met the
// pull request, and a run then waits for the user to ask. The caller holds
// e.mu and starts the run with the key it returns, if any.
func (e *Engine) maybeTendLocked(viewer string, prev, next *model.Mine) bool {
	items := triggers(viewer, next)
	if prev == nil {
		next.Seen = strings.Join(items, "\n")
		return false
	}
	// An item counts as seen only once a run has covered it, so one that
	// arrives while no run can start still starts one later.
	next.Tend, next.AutoRuns, next.Seen = prev.Tend, prev.AutoRuns, prev.Seen
	seen := map[string]bool{}
	for _, s := range strings.Split(prev.Seen, "\n") {
		seen[s] = true
	}
	fresh := false
	for _, it := range items {
		if !seen[it] {
			fresh = true
		}
	}
	if !fresh || e.tendMode() == TendOff || e.Branches == nil || e.tending[next.PR.Key()] || next.AutoRuns >= maxAutoRuns {
		return false
	}
	if t := next.Tend; t != nil && waiting(t) > 0 {
		// Steps still wait for the user; a new plan would pile on them.
		return false
	}
	next.Seen = strings.Join(items, "\n")
	return true
}

func waiting(t *model.Tend) int {
	n := 0
	for _, a := range t.Actions {
		if a.State == model.StateWaiting {
			n++
		}
	}
	return n
}

// Tend starts a run on an own pull request because the user asked.
func (e *Engine) Tend(key string) error {
	return e.startTend(key, nil, false)
}

func (e *Engine) startTend(key string, notes []string, auto bool) error {
	mode := e.tendMode()
	if mode == TendOff {
		return errors.New("the agent is turned off for your pull requests: change it in Settings")
	}
	if e.Branches == nil || e.Inbox == nil {
		return errors.New("this build cannot work on pull requests")
	}
	command, agentName := "", ""
	if e.Agent != nil {
		command, agentName = e.Agent()
	}
	if command == "" {
		return errors.New("no agent is set up: pick one in Settings")
	}
	e.mu.Lock()
	e.loadMineLocked()
	m := e.mine[key]
	if m == nil {
		e.mu.Unlock()
		return errors.New("no such pull request of yours")
	}
	if e.tending[key] {
		e.mu.Unlock()
		return errors.New("the agent is already working on it")
	}
	if m.Tend != nil && waiting(m.Tend) > 0 {
		e.mu.Unlock()
		return errors.New("steps from the last run still wait for you: approve or send them back first")
	}
	if auto && m.AutoRuns >= maxAutoRuns {
		e.mu.Unlock()
		return errors.New("the agent has run on its own enough times in a row")
	}
	e.tending[key] = true
	if auto {
		m.AutoRuns++
	} else {
		m.AutoRuns = 0
		m.Seen = strings.Join(triggers(e.viewer, m), "\n")
	}
	run, earlier := 1, []model.TendAction(nil)
	if old := m.Tend; old != nil {
		if notes == nil {
			notes = old.Notes
		}
		prev := max(old.Run, 1)
		run = prev + 1
		earlier = append(earlier, old.Earlier...)
		for _, a := range old.Actions {
			a.ID = fmt.Sprintf("%d.%s", prev, a.ID)
			earlier = append(earlier, a)
		}
		if len(earlier) > maxEarlier {
			earlier = earlier[len(earlier)-maxEarlier:]
		}
	}
	m.Tend = &model.Tend{Status: "running", Activity: "Starting", Head: m.PR.HeadSHA, At: time.Now(), Notes: notes, Agent: agentName,
		Trigger: blockerWords(m.Blockers), Run: run, Earlier: earlier}
	e.saveMineLocked(m)
	snapshot := clone(m)
	ctx := e.runCtx
	e.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runTend(ctx, key, snapshot, command, agentName, mode)
	}()
	return nil
}

func blockerWords(bs []model.Blocker) string {
	var parts []string
	for _, b := range bs {
		if b.OnYou {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
}

func (e *Engine) saveMineLocked(m *model.Mine) {
	if err := config.WriteJSON(e.minePath(m.PR), m); err != nil {
		e.Logf("could not save %s: %v", m.PR.Key(), err)
	}
	e.Hub.Send(Event{Type: "mine", Key: m.PR.Key()})
}

// updateTend changes the run on key under the lock and saves it.
func (e *Engine) updateTend(key string, f func(m *model.Mine, t *model.Tend)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.mine[key]
	if m == nil || m.Tend == nil {
		return
	}
	f(m, m.Tend)
	e.saveMineLocked(m)
}

func (e *Engine) tendActivity(key, text string) {
	text = strings.TrimSpace(text)
	if len(text) > 140 {
		text = text[:140] + "…"
	}
	e.mu.Lock()
	if m := e.mine[key]; m != nil && m.Tend != nil {
		m.Tend.Activity = text
	}
	e.mu.Unlock()
	e.Hub.Send(Event{Type: "tend", Key: key, Text: text})
}

func toBranch(b repo.Branch) model.TendBranch {
	return model.TendBranch{Dir: b.Dir, Work: b.Work, Scratch: b.Scratch, Out: b.Out, Start: b.Start, Remote: b.Remote, GitDir: b.GitDir}
}

func fromBranch(b model.TendBranch) repo.Branch {
	return repo.Branch{Dir: b.Dir, Work: b.Work, Scratch: b.Scratch, Out: b.Out, Start: b.Start, Remote: b.Remote, GitDir: b.GitDir}
}

// tendPolicy lets the agent read its inputs and the checkout, write and run
// only in the checkout and scratch, and write its plan.
func tendPolicy(b repo.Branch) *acp.Policy {
	return &acp.Policy{Job: "tend", Read: []string{b.Dir}, Write: []string{b.Work, b.Scratch, b.Out}, Exec: []string{b.Work, b.Scratch}}
}

func (e *Engine) runTend(parent context.Context, key string, m model.Mine, command, agentName, mode string) {
	ctx, cancel := context.WithTimeout(parent, tendTimeout)
	defer cancel()
	moved := false
	defer func() {
		e.mu.Lock()
		delete(e.tending, key)
		e.mu.Unlock()
		if moved && parent.Err() == nil {
			// Plan once more from the new head; the auto-run cap stops a loop.
			note := "The branch moved on GitHub while you worked, so your commits were not pushed. Plan again from the new head."
			if err := e.startTend(key, []string{note}, true); err != nil {
				e.Logf("%s: no new run after the branch moved: %v", key, err)
			}
		}
	}()
	failed := func(err error) {
		e.Logf("%s: tend: %v", key, err)
		e.updateTend(key, func(_ *model.Mine, t *model.Tend) { t.Status, t.Error, t.Activity = "failed", err.Error(), "" })
	}
	pr := m.PR
	b, err := e.Branches.PrepareBranch(ctx, pr, m.State)
	if err != nil {
		failed(fmt.Errorf("could not check out the branch: %w", err))
		return
	}
	e.updateTend(key, func(_ *model.Mine, t *model.Tend) { t.Branch, t.Tip = toBranch(b), b.Start })

	checks := make([]map[string]any, 0, len(m.Checks))
	for _, c := range m.Checks {
		row := map[string]any{"id": c.ID, "name": c.Name, "workflow": c.Workflow, "status": c.Status, "conclusion": c.Conclusion, "required": c.Required}
		if c.Failed() && c.JobID != 0 {
			if text, err := e.Inbox.FailedLog(ctx, pr, c.JobID); err == nil {
				row["log"] = text
			} else {
				row["log"] = "Docket could not read this log: " + err.Error()
			}
		}
		checks = append(checks, row)
	}
	inputs := map[string]any{
		"pr.json": map[string]any{
			"repository": pr.Owner + "/" + pr.Repo, "number": pr.Number, "title": pr.Title, "description": pr.Body,
			"base": pr.BaseRef, "head": pr.HeadRef, "headSha": pr.HeadSHA, "blockers": m.Blockers,
		},
		"conversation.json": m.Conversation,
		"checks.json":       checks,
	}
	if m.Tend != nil && len(m.Tend.Notes) > 0 {
		inputs["notes.json"] = m.Tend.Notes
	}
	for name, v := range inputs {
		if err := config.WriteJSON(filepath.Join(b.Dir, name), v); err != nil {
			failed(err)
			return
		}
	}
	planPath := filepath.Join(b.Out, "plan.json")
	_ = os.Remove(planPath)

	e.tendActivity(key, "Starting "+agentName)
	var stderr bytes.Buffer
	client, err := acp.Start(ctx, command, b.Work, &stderr)
	if err != nil {
		failed(fmt.Errorf("could not start %s: %w%s", agentName, err, tail(stderr.String())))
		return
	}
	defer client.Close()
	session, err := client.NewSession(ctx, b.Work)
	if err != nil {
		failed(fmt.Errorf("%s would not open a session: %w%s", agentName, err, tail(stderr.String())))
		return
	}
	client.SetPolicy(session, tendPolicy(b))
	h := &handler{runCode: e.RunCode(), onActivity: func(s string) { e.tendActivity(key, s) }}

	threads, checkIDs := map[string]bool{}, map[string]bool{}
	for _, t := range m.Conversation.Threads {
		threads[t.ID] = true
	}
	for _, c := range m.Checks {
		checkIDs[c.ID] = true
	}
	onTop := func(sha string) bool { return e.Branches.OnTop(ctx, b, sha) }
	prompt := tend.Prompt(tend.PromptData{Dir: b.Dir, Work: b.Work, Scratch: b.Scratch, Out: b.Out, Head: b.Start,
		Login: e.Viewer(), RunCode: e.RunCode(), Notes: inputs["notes.json"] != nil})
	var plan *model.TendPlan
	for attempt := 0; ; attempt++ {
		stop, err := client.Prompt(ctx, session, prompt, h)
		if err != nil {
			failed(fmt.Errorf("%s stopped before finishing: %w%s", agentName, err, tail(stderr.String())))
			return
		}
		var problems []string
		raw, rerr := os.ReadFile(planPath)
		if rerr != nil {
			problems = []string{"there is no file at " + planPath + " (the agent stopped with: " + stop + ")"}
		} else if plan, err = tend.Parse(raw); err != nil {
			problems = []string{err.Error()}
		} else {
			problems = tend.Validate(plan, threads, checkIDs, onTop)
		}
		if len(problems) == 0 {
			// Pin each commit to its SHA, so what is judged is what is pushed.
			for i := range plan.Actions {
				a := &plan.Actions[i]
				if a.Commit == "" {
					continue
				}
				if sha, err := e.Branches.Resolve(ctx, b, a.Commit); err != nil {
					problems = append(problems, fmt.Sprintf("action %s: %v", a.ID, err))
				} else {
					a.Commit = sha
				}
			}
		}
		if len(problems) == 0 {
			break
		}
		if attempt >= maxRetries {
			failed(fmt.Errorf("%s wrote a plan Docket could not use after %d tries: %s", agentName, attempt+1, strings.Join(problems, "; ")))
			return
		}
		e.tendActivity(key, "Asking the agent to fix its plan")
		prompt = tend.RetryPrompt(b.Out, problems)
	}

	e.tendActivity(key, "Deciding what can run without you")
	actions := plan.Actions
	from := b.Start
	chainHeld := ""
	for i := range actions {
		a := &actions[i]
		a.State = model.StateWaiting
		if a.Kind == model.ActFix || a.Kind == model.ActMergeBase {
			a.Patch, _ = e.Branches.Patch(ctx, b, from, a.Commit)
			from = a.Commit
		}
		v := e.judge(ctx, client, b, mode, m, actions, i, chainHeld)
		a.Verdict = &v
		if (a.Kind == model.ActFix || a.Kind == model.ActMergeBase) && v.Decision == model.GateAsk && chainHeld == "" {
			chainHeld = a.ID
		}
	}

	e.updateTend(key, func(_ *model.Mine, t *model.Tend) {
		t.Summary, t.Actions, t.Model, t.Activity = plan.Summary, actions, client.Model(), "Carrying out what can run"
	})
	for _, a := range actions {
		if a.Verdict.Decision == model.GateAuto {
			if err := e.carryOut(ctx, key, a.ID, ""); err != nil {
				e.Logf("%s: %s: %v", key, a.ID, err)
				if errors.Is(err, errBranchMoved) {
					moved = true
				}
			}
		}
	}

	e.reread(key)
	var done, asks int
	e.updateTend(key, func(_ *model.Mine, t *model.Tend) {
		// Ready and free in one step, so an approval sent the moment the
		// page shows the run as ready is not refused as busy.
		delete(e.tending, key)
		t.Status, t.Activity, t.At = "ready", "", time.Now()
		for _, a := range t.Actions {
			switch a.State {
			case model.StateDone:
				done++
			case model.StateWaiting:
				asks++
			}
		}
	})
	if e.Notify != nil && done+asks > 0 {
		var parts []string
		if done > 0 {
			parts = append(parts, fmt.Sprintf("Your agent did %d step%s.", done, plural(done)))
		}
		if asks > 0 {
			parts = append(parts, fmt.Sprintf("%d wait%s for you.", asks, verbS(asks)))
		}
		e.Notify(fmt.Sprintf("%s/%s #%d", pr.Owner, pr.Repo, pr.Number), strings.Join(parts, " "), key)
	}
}

func verbS(n int) string {
	if n == 1 {
		return "s"
	}
	return ""
}

// reread reads an own pull request again after Docket changed it.
func (e *Engine) reread(key string) {
	pr, _, ok := e.prFor(key)
	if !ok {
		return
	}
	pr.UpdatedAt = time.Time{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := e.refreshMine(ctx, pr, false); err != nil {
		e.Logf("could not read %s again: %v", key, err)
	}
}

// judge gives one action its verdict: the mode, the chain and the floors
// first, then the classifier.
func (e *Engine) judge(ctx context.Context, client *acp.Client, b repo.Branch, mode string, m model.Mine, actions []model.TendAction, i int, chainHeld string) model.Verdict {
	a := actions[i]
	if mode == TendAsk {
		return model.Verdict{Decision: model.GateAsk, Reason: "You asked to approve every step.", Floor: true}
	}
	if chainHeld != "" && (a.Kind == model.ActFix || a.Kind == model.ActMergeBase) {
		return model.Verdict{Decision: model.GateAsk, Reason: "It builds on " + chainHeld + ", which waits for you.", Floor: true}
	}
	fixedAlone := false
	for _, other := range actions[:i] {
		if other.Kind != model.ActFix && other.Kind != model.ActMergeBase || !contains(other.Threads, a.Thread) || other.Verdict == nil {
			continue
		}
		// A reply or resolve about a fix that waits must wait with it.
		if other.Verdict.Decision == model.GateAsk && (a.Kind == model.ActReply || a.Kind == model.ActResolve) {
			return model.Verdict{Decision: model.GateAsk, Reason: "It is about " + other.ID + ", which waits for you.", Floor: true}
		}
		fixedAlone = true
	}
	// Saying a request is done, or closing its thread, goes ahead alone only
	// when a fix in this plan that goes ahead alone answers that thread.
	if (a.Kind == model.ActResolve || a.Kind == model.ActReply && a.Stance == "agrees") && !fixedAlone {
		return model.Verdict{Decision: model.GateAsk, Reason: "No step in this plan that runs on its own fixes this thread.", Floor: true}
	}
	if v, held := tend.Floor(a, a.Patch); held {
		return v
	}
	note := ""
	run := func(ctx context.Context, prompt string) (string, error) {
		// An empty folder, so no instructions file the agent wrote in the
		// checkout reaches the session that judges it.
		dir, err := os.MkdirTemp("", "docket-gate-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(dir)
		session, err := client.NewSession(ctx, dir)
		if err != nil {
			return "", err
		}
		// The classifier reads only what it is shown.
		client.SetPolicy(session, &acp.Policy{Job: "gate"})
		if id, ok := acp.Smallest(client.Models()); ok {
			if err := client.SetModel(ctx, session, id); err != nil {
				note = " The agent would not switch to its smaller model, so its default model judged."
			}
		}
		h := &handler{}
		if _, err := client.Prompt(ctx, session, prompt, h); err != nil {
			return "", err
		}
		return h.reply(), nil
	}
	v := tend.Classify(ctx, run, a, a.Patch, gateContext(m, a, actions[:i]))
	v.Model = client.Model()
	v.Reason += note
	return v
}

// gateContext is the text an action answers, its threads and its checks,
// and the steps before it in the plan with their verdicts.
func gateContext(m model.Mine, a model.TendAction, before []model.TendAction) string {
	want := map[string]bool{a.Thread: a.Thread != ""}
	for _, t := range a.Threads {
		want[t] = true
	}
	var b strings.Builder
	for _, t := range m.Conversation.Threads {
		if !want[t.ID] {
			continue
		}
		fmt.Fprintf(&b, "Thread on %s:%d\n", t.Path, t.Line)
		for _, c := range t.Comments {
			fmt.Fprintf(&b, "  %s: %s\n", c.Author, c.Body)
		}
	}
	for _, id := range a.Checks {
		for _, c := range m.Checks {
			if c.ID == id {
				fmt.Fprintf(&b, "Check %s: %s %s\n", c.Name, c.Status, c.Conclusion)
			}
		}
	}
	for i, o := range before {
		if i == 0 {
			b.WriteString("Earlier steps in this plan:\n")
		}
		verdict := "not judged"
		if o.Verdict != nil {
			verdict = o.Verdict.Decision
		}
		fmt.Fprintf(&b, "  %s %s (threads %s): %s [%s]\n", o.ID, o.Kind, strings.Join(append(append([]string(nil), o.Threads...), o.Thread), " "), o.Why, verdict)
	}
	if b.Len() == 0 {
		return "(nothing beyond the step itself)"
	}
	return b.String()
}

// carryOut does one action. body, when not empty, replaces a reply's text.
func (e *Engine) carryOut(ctx context.Context, key, id, body string) error {
	e.mu.Lock()
	m := e.mine[key]
	if m == nil || m.Tend == nil {
		e.mu.Unlock()
		return errors.New("no such run")
	}
	t := m.Tend
	var a model.TendAction
	found := false
	for i := range t.Actions {
		if x := &t.Actions[i]; x.ID == id {
			a, found = *x, true
			if x.State == model.StateWaiting {
				// Claimed under the lock, so a second click finds it taken.
				x.State = model.StateWorking
			}
		}
	}
	pr, checks, tip, b := m.PR, m.Checks, t.Tip, fromBranch(t.Branch)
	e.mu.Unlock()
	if !found {
		return errors.New("no such step")
	}
	if a.State != model.StateWaiting {
		return fmt.Errorf("that step is already %s", a.State)
	}
	if strings.TrimSpace(body) != "" && a.Kind == model.ActReply {
		a.Body = strings.TrimSpace(body)
	}

	var err error
	undo, result, newTip := "", "", tip
	switch a.Kind {
	case model.ActFix, model.ActMergeBase:
		on := b
		on.Start = tip
		if !e.Branches.OnTop(ctx, on, a.Commit) {
			err = errors.New("it builds on a step that has not been pushed: approve that one first")
			break
		}
		err = e.Branches.Push(ctx, b, pr, tip, a.Commit)
		if errors.Is(err, repo.ErrMoved) {
			err = errBranchMoved
		}
		undo, newTip, result = tip, a.Commit, "Pushed "+short(a.Commit)+"."
	case model.ActReply:
		var cid string
		cid, err = e.Inbox.Reply(ctx, pr, a.Thread, a.Body)
		undo, result = cid, "Replied."
	case model.ActResolve:
		err = e.Inbox.Resolve(ctx, pr, a.Thread, true)
		undo, result = a.Thread, "Resolved."
	case model.ActRerun:
		runs := map[int64]bool{}
		for _, id := range a.Checks {
			for _, c := range checks {
				if c.ID == id && c.RunID != 0 {
					runs[c.RunID] = true
				}
			}
		}
		if len(runs) == 0 {
			err = errors.New("that check is not a GitHub Actions job, so it cannot be re-run from here")
		}
		for run := range runs {
			if err == nil {
				err = e.Inbox.Rerun(ctx, pr, run)
			}
		}
		result = "Re-ran the failed jobs."
	case model.ActUpdateBranch:
		err = e.Inbox.UpdateBranch(ctx, pr)
		result = "Updated the branch from its base."
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	m = e.mine[key]
	if m == nil || m.Tend == nil {
		return err
	}
	for i := range m.Tend.Actions {
		x := &m.Tend.Actions[i]
		if x.ID != id {
			continue
		}
		x.At = time.Now()
		x.Body = a.Body
		if err != nil {
			x.State, x.Result = model.StateFailed, err.Error()
		} else {
			x.State, x.Result, x.Undo = model.StateDone, result, undo
		}
	}
	if err == nil && newTip != tip {
		m.Tend.Tip = newTip
		m.PR.HeadSHA = newTip
	}
	e.saveMineLocked(m)
	return err
}

func short(sha string) string {
	if len(sha) > 7 && !strings.HasPrefix(sha, "demo-") {
		return sha[:7]
	}
	return sha
}

// Decide answers a step that waits for the user: approve carries it out
// (with body as the edited reply text), send-back rejects it with a note
// for the agent's next run.
func (e *Engine) Decide(ctx context.Context, key, id, choice, body, note string) error {
	if choice != "approve" && choice != "send-back" {
		return fmt.Errorf("unknown choice %q", choice)
	}
	if e.busy(key) {
		return errors.New("the agent is still working on this pull request: wait for it to finish")
	}
	switch choice {
	case "approve":
		e.resetAuto(key)
		err := e.carryOut(ctx, key, id, body)
		e.reread(key)
		return err
	case "send-back":
	default:
		return fmt.Errorf("unknown choice %q", choice)
	}
	replan := false
	e.mu.Lock()
	m := e.mine[key]
	if m == nil || m.Tend == nil {
		e.mu.Unlock()
		return errors.New("no such run")
	}
	t := m.Tend
	idx := -1
	for i, a := range t.Actions {
		if a.ID == id {
			idx = i
		}
	}
	if idx < 0 || t.Actions[idx].State != model.StateWaiting {
		e.mu.Unlock()
		return errors.New("that step is not waiting for you")
	}
	a := &t.Actions[idx]
	a.State, a.At = model.StateRejected, time.Now()
	text := fmt.Sprintf("You proposed %s (%s): %s", a.ID, a.Kind, a.Why)
	if n := strings.TrimSpace(note); n != "" {
		text += " The author sent it back: " + n
	} else {
		text += " The author sent it back without a note."
	}
	t.Notes = append(t.Notes, text)
	if a.Kind == model.ActFix || a.Kind == model.ActMergeBase {
		// Later commits are built on this one, so they go with it.
		for i := idx + 1; i < len(t.Actions); i++ {
			x := &t.Actions[i]
			if x.State == model.StateWaiting && (x.Kind == model.ActFix || x.Kind == model.ActMergeBase || contains(a.Threads, x.Thread)) {
				x.State, x.Result = model.StateSkipped, "It depended on "+a.ID+", which you sent back."
			}
		}
	}
	m.AutoRuns = 0
	replan = waiting(t) == 0
	notes := append([]string(nil), t.Notes...)
	e.saveMineLocked(m)
	e.mu.Unlock()
	if replan {
		return e.startTend(key, notes, false)
	}
	return nil
}

func (e *Engine) busy(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tending[key]
}

func (e *Engine) resetAuto(key string) {
	e.mu.Lock()
	if m := e.mine[key]; m != nil {
		m.AutoRuns = 0
	}
	e.mu.Unlock()
}

// Undo reverses a step Docket carried out: a pushed commit is reverted by a
// new commit on top, a reply is deleted, a resolve is reopened.
func (e *Engine) Undo(ctx context.Context, key, id string) error {
	e.mu.Lock()
	m := e.mine[key]
	if m == nil || m.Tend == nil {
		e.mu.Unlock()
		return errors.New("no such run")
	}
	if e.tending[key] {
		e.mu.Unlock()
		return errors.New("the agent is working on this pull request: wait for it to finish")
	}
	x := findAction(m.Tend, id)
	if x == nil || x.State != model.StateDone {
		e.mu.Unlock()
		return errors.New("only a step that was carried out can be undone")
	}
	a := *x
	x.State = model.StateWorking
	m.AutoRuns = 0
	pr, tip, b := m.PR, m.Tend.Tip, fromBranch(m.Tend.Branch)
	e.mu.Unlock()
	restore := func() {
		e.updateTend(key, func(_ *model.Mine, t *model.Tend) {
			if x := findAction(t, id); x != nil && x.State == model.StateWorking {
				x.State = model.StateDone
			}
		})
	}

	var err error
	newTip := tip
	switch a.Kind {
	case model.ActFix, model.ActMergeBase:
		var rev string
		if rev, err = e.Branches.Revert(ctx, b, tip, a.Commit); err == nil {
			if err = e.Branches.Push(ctx, b, pr, tip, rev); err == nil {
				newTip = rev
			}
		}
	case model.ActReply:
		err = e.Inbox.DeleteComment(ctx, pr, a.Undo)
	case model.ActResolve:
		err = e.Inbox.Resolve(ctx, pr, a.Thread, false)
	default:
		restore()
		return errors.New("that step cannot be undone")
	}
	if err != nil {
		restore()
		return err
	}
	e.updateTend(key, func(m *model.Mine, t *model.Tend) {
		if x := findAction(t, id); x != nil {
			x.State, x.Result, x.At = model.StateUndone, "Undone.", time.Now()
		}
		if newTip != tip {
			t.Tip, m.PR.HeadSHA = newTip, newTip
		}
	})
	e.reread(key)
	return nil
}

// findAction finds a step of this run or an earlier one.
func findAction(t *model.Tend, id string) *model.TendAction {
	for i := range t.Actions {
		if t.Actions[i].ID == id {
			return &t.Actions[i]
		}
	}
	for i := range t.Earlier {
		if t.Earlier[i].ID == id {
			return &t.Earlier[i]
		}
	}
	return nil
}
