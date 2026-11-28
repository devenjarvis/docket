package review

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/devenjarvis/docket/internal/acp"
	"github.com/devenjarvis/docket/internal/brief"
	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/diff"
	"github.com/devenjarvis/docket/internal/ledger"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/owners"
	"github.com/devenjarvis/docket/internal/repo"
)

// readTimeout bounds one read. A change that takes longer than this to
// prepare is one the reviewer should be told about, not kept waiting on.
const readTimeout = 40 * time.Minute

// maxRetries is how many times a brief that fails validation goes back.
const maxRetries = 2

func (e *Engine) setActivity(key, text string) {
	text = strings.TrimSpace(text)
	if len(text) > 140 {
		text = text[:140] + "…"
	}
	e.mu.Lock()
	if r := e.reviews[key]; r != nil {
		r.Activity = text
	}
	e.mu.Unlock()
	e.Hub.Send(Event{Type: "activity", Key: key, Text: text})
}

func (e *Engine) fail(key string, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.reviews[key]
	if r == nil {
		return
	}
	r.Status, r.Error, r.Activity = "failed", err.Error(), ""
	e.Logf("%s: %v", key, err)
	e.save(r)
}

// read has an agent prepare one pull request.
func (e *Engine) read(parent context.Context, key string) {
	e.mu.Lock()
	e.waitSweep(key)
	r := e.reviews[key]
	if r == nil || (r.Status != "queued") {
		e.mu.Unlock()
		return
	}
	command, agentName := "", ""
	if e.Agent != nil {
		command, agentName = e.Agent()
	}
	ctx, cancel := context.WithTimeout(parent, readTimeout)
	e.cancels[key] = cancel
	r.Status, r.Error = "reading", ""
	pr := r.PR
	self := r.Self
	var previous *model.Review
	if r.Brief != nil && r.ReadSHA != "" && r.ReadSHA != pr.HeadSHA {
		cp := *r
		previous = &cp
	}
	e.save(r)
	e.mu.Unlock()
	defer func() {
		cancel()
		e.mu.Lock()
		delete(e.cancels, key)
		e.mu.Unlock()
	}()

	if command == "" {
		e.fail(key, errors.New("no agent is set up: pick one in Settings"))
		return
	}
	started := time.Now()
	repoName := pr.Owner + "/" + pr.Repo
	runCode := e.runsIn(repoName)
	var client *acp.Client
	entry := ledger.Entry{Kind: ledger.KindRead, Repo: repoName, Number: pr.Number, Agent: agentName, Reread: previous != nil}
	if self {
		entry.Kind = ledger.KindSelf
	}
	recorded := false
	defer func() {
		// A read that ended any way but done is recorded as failed, unless
		// a newer push or a re-read superseded it, or the app is quitting.
		if recorded || parent.Err() != nil || errors.Is(ctx.Err(), context.Canceled) {
			return
		}
		entry.Failed, entry.Seconds = true, int(time.Since(started).Seconds())
		if client != nil {
			entry.Model, entry.Usage = client.Model(), client.Usage()
		}
		e.record(entry)
	}()
	e.setActivity(key, "Checking out the branch")
	ws, diffText, err := e.Prep.Prepare(ctx, pr)
	if err != nil {
		e.fail(key, fmt.Errorf("could not check out the pull request: %w", err))
		return
	}
	files := diff.Parse(diffText)
	if len(files) == 0 {
		e.fail(key, errors.New("this pull request has no changes to read"))
		return
	}

	// Which files fall to this reviewer.
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	slice, narrowed := owners.Load(ws.Head).Slice(paths, e.Viewer(), nil)
	if self || !narrowed || len(slice) == len(paths) {
		// The author answers for all of their own change.
		slice = nil
	}

	ruleList, _ := e.Rules.ForRepo(pr.Owner + "/" + pr.Repo)
	ruleIDs := map[string]bool{}
	for _, rl := range ruleList {
		ruleIDs[rl.ID] = true
	}
	m := loadMap(e.Home)
	elsewhere := map[string][]string{}
	for rp, areas := range m {
		if rp == pr.Owner+"/"+pr.Repo {
			continue
		}
		for a := range areas {
			elsewhere[rp] = append(elsewhere[rp], a)
		}
	}
	inputs := map[string]any{
		"diff.patch": diffText,
		"pr.json": map[string]any{
			"repository": pr.Owner + "/" + pr.Repo, "number": pr.Number, "title": pr.Title,
			"description": pr.Body, "author": pr.Author, "base": pr.BaseRef, "head": pr.HeadRef, "headSha": pr.HeadSHA,
		},
		"rules.json": ruleList,
		"reviewer.json": map[string]any{
			"login": e.Viewer(), "askedBecause": pr.Why, "slice": slice,
			"knowsThisRepository": m[pr.Owner+"/"+pr.Repo],
			"knowsElsewhere":      elsewhere,
			"note":                "knowsThisRepository counts reviews done through this tool only; an area that is missing has not been reviewed here.",
		},
	}
	if self {
		inputs["reviewer.json"] = map[string]any{"login": e.Viewer(), "role": "author",
			"note": "This is the author's own pull request. They read this brief before anyone reviews it."}
	}
	inputs["conversation.json"] = e.conversationFor(ctx, pr)
	if previous != nil {
		inputs["previous.json"] = map[string]any{"brief": previous.Brief, "answers": previous.Answers}
	}
	for name, v := range inputs {
		p := filepath.Join(ws.Dir, name)
		if s, ok := v.(string); ok {
			err = os.WriteFile(p, []byte(s), 0o644)
		} else {
			err = config.WriteJSON(p, v)
		}
		if err != nil {
			e.fail(key, err)
			return
		}
	}
	briefPath := filepath.Join(ws.Out, "brief.json")
	_ = os.Remove(briefPath)

	e.setActivity(key, "Starting "+agentName)
	var stderr bytes.Buffer
	client, err = acp.Start(ctx, command, ws.Head, &stderr)
	if err != nil {
		client = nil
		e.fail(key, fmt.Errorf("could not start %s (%s): %w%s", agentName, command, err, tail(stderr.String())))
		return
	}
	defer client.Close()
	session, err := client.NewSession(ctx, ws.Head)
	if err != nil {
		e.fail(key, fmt.Errorf("%s would not open a session: %w%s", agentName, err, tail(stderr.String())))
		return
	}
	client.SetPolicy(session, acp.ReviewPolicy(ws.Dir, ws.Head, ws.Base, ws.Scratch, ws.Out))
	h := &handler{runCode: runCode, onActivity: func(s string) { e.setActivity(key, s) }}

	prompt := brief.Prompt(brief.PromptData{Workspace: ws, RunCode: runCode, Slice: slice != nil, Previous: previous != nil, Self: self})
	var b *model.Brief
	for attempt := 0; ; attempt++ {
		entry.Tries = attempt + 1
		stop, err := client.Prompt(ctx, session, prompt, h)
		if err != nil {
			if ctx.Err() != nil && parent.Err() == nil && errors.Is(ctx.Err(), context.Canceled) {
				return // superseded by a newer push or a re-read
			}
			e.fail(key, fmt.Errorf("%s stopped before finishing: %w%s", agentName, err, tail(stderr.String())))
			return
		}
		var problems []string
		raw, rerr := os.ReadFile(briefPath)
		switch {
		case rerr != nil:
			problems = []string{"there is no file at " + briefPath + " (the agent stopped with: " + stop + ")"}
		default:
			b, err = brief.Parse(raw)
			if err != nil {
				problems = []string{err.Error()}
			} else {
				problems = brief.Validate(b, files, ruleIDs)
			}
		}
		if len(problems) == 0 {
			break
		}
		if attempt >= maxRetries {
			e.fail(key, fmt.Errorf("%s wrote a brief Docket could not use after %d tries: %s", agentName, attempt+1, strings.Join(problems, "; ")))
			return
		}
		e.setActivity(key, "Asking the agent to fix its brief")
		prompt = brief.RetryPrompt(ws.Out, problems)
	}
	brief.Fill(b, files)

	e.mu.Lock()
	r = e.reviews[key]
	if r == nil {
		e.mu.Unlock()
		return
	}
	// Answers carry over only for decisions that still stand and still
	// offer the option the reviewer chose.
	keep := map[string]model.Answer{}
	for _, d := range b.Decisions {
		if a, ok := r.Answers[d.ID]; ok {
			for _, o := range d.Options {
				if o.ID == a.Option {
					keep[d.ID] = a
				}
			}
		}
	}
	r.Answers = keep
	r.Brief, r.ReadSHA, r.Slice = b, pr.HeadSHA, slice
	r.Status, r.Activity, r.Error = "ready", "", ""
	r.Agent, r.Model = agentName, client.Model()
	r.ReadAt, r.ReadTook = time.Now(), int(time.Since(started).Seconds())
	r.Isolation = client.Isolation().State
	r.Posted = nil
	delete(e.diffs, key+"@"+pr.HeadSHA)
	sum := summarize(r)
	// Read under the lock: once it is released, the user's answers change
	// these.
	entry.Model, entry.Seconds = r.Model, r.ReadTook
	entry.Carried, entry.Isolation = len(keep), r.Isolation
	e.save(r)
	e.mu.Unlock()

	entry.Usage = client.Usage()
	entry.NeedLines, entry.TotalLines, entry.Decisions = sum.NeedLines, sum.TotalLines, sum.Decisions
	for _, c := range b.Claims {
		switch c.Status {
		case model.Proved:
			entry.Proved++
		case model.Contradicted:
			entry.Contradicted++
		case model.Unproven:
			entry.Unproven++
		}
	}
	for _, sec := range b.Sections {
		if sec.Kind == model.Mechanical {
			entry.Mechanical++
		}
	}
	recorded = true
	e.record(entry)

	if self {
		body := "Nothing a reviewer would stop on."
		if sum.Decisions > 0 {
			body = fmt.Sprintf("%d finding%s to settle before review.", sum.Decisions, plural(sum.Decisions))
		}
		e.notify(config.NotifyRead, fmt.Sprintf("Self-review of %s/%s #%d", pr.Owner, pr.Repo, pr.Number), body, key)
	} else {
		body := "Nothing needs your judgment."
		if sum.Decisions > 0 {
			body = fmt.Sprintf("%d decision%s for you, about %d min.", sum.Decisions, plural(sum.Decisions), max(sum.Minutes, 1))
		}
		e.notify(config.NotifyRead, fmt.Sprintf("%s/%s #%d is ready", pr.Owner, pr.Repo, pr.Number), body, key)
	}
	// Earlier heads of this PR are no longer needed.
	if previous != nil {
		old := previous.PR
		old.HeadSHA = previous.ReadSHA
		e.Prep.Release(context.Background(), old, repo.Layout(e.Home, old))
	}
}

// conversationFor reads what others have said on a pull request for the
// agent. A failure to read it is not worth failing the read over: the agent
// gets an empty conversation and the reason goes to the log.
func (e *Engine) conversationFor(ctx context.Context, pr model.PR) model.Conversation {
	if e.Inbox == nil {
		return model.Conversation{}
	}
	_, _, conv, err := e.Inbox.Detail(ctx, pr.Owner, pr.Repo, pr.Number)
	if err != nil {
		e.Logf("could not read the conversation on %s: %v", pr.Key(), err)
		return model.Conversation{}
	}
	return conv
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return "\n" + strings.Join(lines, "\n")
}

// handler turns an agent's session traffic into a progress line and the
// text of its reply.
type handler struct {
	runCode    bool
	onActivity func(string)
	onText     func(string)

	mu   sync.Mutex
	text strings.Builder
}

func (h *handler) Update(u acp.SessionUpdate) {
	switch u.Kind {
	case "tool_call":
		if u.Title != "" && h.onActivity != nil {
			h.onActivity(u.Title)
		}
	case "plan":
		for _, p := range u.Entries {
			if p.Status == "in_progress" && h.onActivity != nil {
				h.onActivity(p.Content)
				break
			}
		}
	case "agent_message_chunk":
		t := u.Text()
		h.mu.Lock()
		h.text.WriteString(t)
		h.mu.Unlock()
		if h.onText != nil && t != "" {
			h.onText(t)
		}
	}
}

func (h *handler) Activity(a acp.Activity) {
	if h.onActivity == nil || a.Denied {
		return
	}
	switch a.Kind {
	case "terminal":
		h.onActivity("Running " + a.Command)
	case "read":
		h.onActivity("Reading " + filepath.Base(a.Path))
	}
}

// RequestPermission answers for the reviewer, who is not there: the agent
// works in throwaway checkouts, so reads, writes and runs inside them are
// allowed, and running code is refused when the reviewer turned that off.
func (h *handler) RequestPermission(_ context.Context, req acp.PermissionRequest) (string, bool) {
	want := "allow"
	if req.ToolCall.Kind == "execute" && !h.runCode {
		want = "reject"
	}
	pick := ""
	for _, o := range req.Options {
		if strings.HasPrefix(o.Kind, want) {
			// Prefer the one-time form: nothing should outlive this job.
			if strings.HasSuffix(o.Kind, "_once") {
				return o.OptionID, true
			}
			pick = o.OptionID
		}
	}
	return pick, pick != ""
}

func (h *handler) reply() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.TrimSpace(h.text.String())
}

// Ask puts a reviewer's follow-up question to the agent. It returns at once;
// the answer arrives as events and is saved on the review.
func (e *Engine) Ask(_ context.Context, key, decision, question string) (string, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "", errors.New("ask something")
	}
	e.mu.Lock()
	r := e.reviews[key]
	if r == nil || r.Brief == nil {
		e.mu.Unlock()
		return "", errors.New("this review is not ready yet")
	}
	command, agentName := "", ""
	if e.Agent != nil {
		command, agentName = e.Agent()
	}
	if command == "" {
		e.mu.Unlock()
		return "", errors.New("no agent is set up")
	}
	id := fmt.Sprintf("q%d", time.Now().UnixNano())
	r.Asks = append(r.Asks, model.AskTurn{ID: id, Decision: decision, Question: question, Pending: true, At: time.Now()})
	about := ""
	for _, d := range r.Brief.Decisions {
		if d.ID == decision {
			about = d.Title
		}
	}
	pr := r.PR
	pr.HeadSHA = r.ReadSHA
	e.save(r)
	e.mu.Unlock()

	go func() {
		// The question outlives the request that asked it.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		ws := repo.Layout(e.Home, pr)
		answer, err := e.askAgent(ctx, command, ws, key, id, about, question)
		if err != nil {
			answer = fmt.Sprintf("%s could not answer: %v", agentName, err)
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		if r := e.reviews[key]; r != nil {
			for i := range r.Asks {
				if r.Asks[i].ID == id {
					r.Asks[i].Answer, r.Asks[i].Pending = answer, false
				}
			}
			e.save(r)
		}
	}()
	return id, nil
}

func (e *Engine) askAgent(ctx context.Context, command string, ws repo.Workspace, key, id, about, question string) (string, error) {
	if _, err := os.Stat(ws.Head); err != nil {
		return "", errors.New("the checkout for this review is gone: read the pull request again first")
	}
	var stderr bytes.Buffer
	client, err := acp.Start(ctx, command, ws.Head, &stderr)
	if err != nil {
		return "", err
	}
	defer client.Close()
	session, err := client.NewSession(ctx, ws.Head)
	if err != nil {
		return "", err
	}
	client.SetPolicy(session, acp.AskPolicy(ws.Dir, ws.Head, ws.Base, ws.Scratch, ws.Out))
	repoName := repoOf(key)
	runCode := e.runsIn(repoName)
	h := &handler{runCode: runCode, onText: func(t string) {
		e.Hub.Send(Event{Type: "ask", Key: key, ID: id, Text: t})
	}}
	started := time.Now()
	_, err = client.Prompt(ctx, session, brief.AskPrompt(ws, about, question, runCode), h)
	en := ledger.Entry{Kind: ledger.KindAsk, Repo: repoName, Number: numberOf(key), Model: client.Model(),
		Seconds: int(time.Since(started).Seconds()), Failed: err != nil, Usage: client.Usage()}
	e.record(en)
	if err != nil {
		return "", err
	}
	if a := h.reply(); a != "" {
		return a, nil
	}
	return "", errors.New("it sent no reply")
}
