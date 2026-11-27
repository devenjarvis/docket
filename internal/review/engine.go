// Package review is Docket's core loop: find the pull requests waiting on
// the user, have an agent read each one, keep the reviewer's decisions, and
// post the review they sign.
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
	"sync"
	"time"

	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/diff"
	"github.com/devenjarvis/docket/internal/gh"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/repo"
	"github.com/devenjarvis/docket/internal/rules"
)

// Engine owns every review.
type Engine struct {
	Home  string
	GH    gh.Client
	Prep  repo.Preparer
	Rules *rules.Store
	Hub   *Hub
	// Agent returns the command that starts the user's ACP agent and its
	// display name; an empty command means none is set up.
	Agent func() (command, name string)
	// RunCode reports whether the agent may execute the repository's code.
	RunCode func() bool
	// Notify, when set, raises a desktop notification.
	Notify func(title, body, key string)
	// Logf receives one line per notable event.
	Logf func(format string, args ...any)

	wg       sync.WaitGroup
	mu       sync.Mutex
	reviews  map[string]*model.Review
	diffs    map[string][]diff.File // key@sha → parsed diff
	queue    chan string
	queued   map[string]bool
	cancels  map[string]context.CancelFunc
	viewer   string
	syncErr  string
	lastSync time.Time
}

// New loads saved reviews from home.
func New(home string, ghc gh.Client, prep repo.Preparer, rs *rules.Store) *Engine {
	e := &Engine{
		Home: home, GH: ghc, Prep: prep, Rules: rs, Hub: NewHub(),
		reviews: map[string]*model.Review{}, diffs: map[string][]diff.File{},
		queue: make(chan string, 256), queued: map[string]bool{},
		cancels: map[string]context.CancelFunc{},
		RunCode: func() bool { return true },
		Logf:    func(string, ...any) {},
	}
	entries, _ := os.ReadDir(e.stateDir())
	for _, en := range entries {
		if !strings.HasSuffix(en.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(e.stateDir(), en.Name()))
		if err != nil {
			continue
		}
		var r model.Review
		if json.Unmarshal(b, &r) != nil || r.PR.Number == 0 {
			continue
		}
		fix(&r)
		// A read that was under way when the app last quit starts over.
		if r.Status == "reading" || r.Status == "queued" {
			r.Status = "queued"
			r.Activity = ""
		}
		e.reviews[r.PR.Key()] = &r
	}
	return e
}

func fix(r *model.Review) {
	if r.Answers == nil {
		r.Answers = map[string]model.Answer{}
	}
	if r.Seconds == nil {
		r.Seconds = map[string]int{}
	}
	if r.Audited == nil {
		r.Audited = map[string]bool{}
	}
}

func (e *Engine) stateDir() string { return filepath.Join(e.Home, "state") }

func (e *Engine) statePath(pr model.PR) string {
	return filepath.Join(e.stateDir(), fmt.Sprintf("%s__%s__%d.json", pr.Owner, pr.Repo, pr.Number))
}

// save persists r and tells the UI. The caller holds e.mu.
func (e *Engine) save(r *model.Review) {
	if err := config.WriteJSON(e.statePath(r.PR), r); err != nil {
		e.Logf("could not save %s: %v", r.PR.Key(), err)
	}
	e.Hub.Send(Event{Type: "review", Key: r.PR.Key()})
}

// Start runs the poller and the reading workers until ctx ends.
func (e *Engine) Start(ctx context.Context, workers int, poll time.Duration) {
	for i := 0; i < workers; i++ {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case key := <-e.queue:
					e.mu.Lock()
					delete(e.queued, key)
					e.mu.Unlock()
					e.read(ctx, key)
				}
			}
		}()
	}
	e.mu.Lock()
	for k, r := range e.reviews {
		if r.Status == "queued" {
			e.enqueueLocked(k)
		}
	}
	e.mu.Unlock()
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		_ = e.Sync(ctx)
		t := time.NewTicker(poll)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = e.Sync(ctx)
			}
		}
	}()
}

// Wait blocks until the goroutines Start began have returned, which they do
// once Start's context ends.
func (e *Engine) Wait() { e.wg.Wait() }

func (e *Engine) enqueueLocked(key string) {
	if e.queued[key] {
		return
	}
	select {
	case e.queue <- key:
		e.queued[key] = true
	default:
	}
}

// Sync asks GitHub which pull requests wait on the user and queues a read
// for each new one and each one whose author has pushed since it was read.
func (e *Engine) Sync(ctx context.Context) error {
	err := e.sync(ctx)
	e.mu.Lock()
	e.lastSync = time.Now()
	if err != nil {
		e.syncErr = err.Error()
	} else {
		e.syncErr = ""
	}
	e.mu.Unlock()
	e.Hub.Send(Event{Type: "queue"})
	return err
}

func (e *Engine) sync(ctx context.Context) error {
	if e.Viewer() == "" {
		v, err := e.GH.Viewer(ctx)
		if err != nil {
			return err
		}
		e.mu.Lock()
		e.viewer = v
		e.mu.Unlock()
	}
	list, err := e.GH.Assigned(ctx)
	if err != nil {
		return err
	}
	waiting := map[string]bool{}
	for _, found := range list {
		key := found.Key()
		waiting[key] = true
		e.mu.Lock()
		cur := e.reviews[key]
		// Skip the detail call when nothing has changed since the last look.
		fresh := cur != nil && cur.PR.HeadSHA != "" && cur.PR.UpdatedAt.Equal(found.UpdatedAt)
		e.mu.Unlock()
		if fresh {
			continue
		}
		full, err := e.GH.PR(ctx, found.Owner, found.Repo, found.Number)
		if err != nil {
			e.Logf("could not load %s: %v", key, err)
			continue
		}
		full.Why = found.Why
		e.upsert(full)
	}
	// A PR that no longer waits on the user leaves the queue, unless they
	// are partway through it or added it by hand.
	e.mu.Lock()
	for key, r := range e.reviews {
		if waiting[key] || contains(r.PR.Why, "added by hand") {
			continue
		}
		if r.Status == "posted" || len(r.Answers) == 0 {
			if c := e.cancels[key]; c != nil {
				c()
			}
			delete(e.reviews, key)
			_ = os.Remove(e.statePath(r.PR))
		}
	}
	e.mu.Unlock()
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// upsert records pr and queues a read when its head is new.
func (e *Engine) upsert(pr model.PR) {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := pr.Key()
	r := e.reviews[key]
	if r == nil {
		r = &model.Review{PR: pr, Status: "queued"}
		fix(r)
		e.reviews[key] = r
		e.save(r)
		e.enqueueLocked(key)
		return
	}
	pushed := r.ReadSHA != "" && r.ReadSHA != pr.HeadSHA
	if len(pr.Why) == 0 {
		pr.Why = r.PR.Why
	}
	r.PR = pr
	if pushed || (r.Status == "failed" && r.ReadSHA == "") {
		if c := e.cancels[key]; c != nil {
			c()
		}
		r.Status = "queued"
		r.Error = ""
		r.Posted = nil
		e.enqueueLocked(key)
	}
	e.save(r)
}

// Add queues a pull request the user named by hand.
func (e *Engine) Add(ctx context.Context, ref string) (string, error) {
	owner, name, n, ok := model.ParseRef(strings.TrimSpace(ref))
	if !ok {
		return "", errors.New("that does not look like a pull request: use owner/repo#123 or its URL")
	}
	pr, err := e.GH.PR(ctx, owner, name, n)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	if cur := e.reviews[pr.Key()]; cur != nil {
		pr.Why = cur.PR.Why
	}
	e.mu.Unlock()
	if !contains(pr.Why, "added by hand") {
		pr.Why = append(pr.Why, "added by hand")
	}
	e.upsert(pr)
	return pr.Key(), nil
}

// Reread queues a fresh read of a PR, keeping the reviewer's answers.
func (e *Engine) Reread(key string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.reviews[key]
	if r == nil {
		return errors.New("no such review")
	}
	if c := e.cancels[key]; c != nil {
		c()
	}
	r.Status, r.Error, r.Activity = "queued", "", ""
	e.save(r)
	e.enqueueLocked(key)
	return nil
}

// Remove drops a review from the queue.
func (e *Engine) Remove(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.reviews[key]
	if r == nil {
		return
	}
	if c := e.cancels[key]; c != nil {
		c()
	}
	delete(e.reviews, key)
	_ = os.Remove(e.statePath(r.PR))
	e.Hub.Send(Event{Type: "queue"})
}

// Viewer is the signed-in GitHub login, once known.
func (e *Engine) Viewer() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.viewer
}

// Summary is one row of the queue.
type Summary struct {
	Key       string   `json:"key"`
	PR        model.PR `json:"pr"`
	Status    string   `json:"status"`
	Error     string   `json:"error,omitempty"`
	Activity  string   `json:"activity,omitempty"`
	Decisions int      `json:"decisions"`
	Answered  int      `json:"answered"`
	// NeedLines is the changed lines in judgment sections; TotalLines is
	// every changed line.
	NeedLines  int    `json:"needLines"`
	TotalLines int    `json:"totalLines"`
	Minutes    int    `json:"minutes"`
	Contra     int    `json:"contradicted"`
	HasContext bool   `json:"hasContext"`
	Pushed     bool   `json:"pushed"`
	Slice      int    `json:"slice"`
	Lane       string `json:"lane"`
}

// Status is everything the queue screen shows.
type Status struct {
	Viewer   string    `json:"viewer"`
	SyncErr  string    `json:"syncError,omitempty"`
	LastSync time.Time `json:"lastSync"`
	Reviews  []Summary `json:"reviews"`
}

func summarize(r *model.Review) Summary {
	s := Summary{Key: r.PR.Key(), PR: r.PR, Status: r.Status, Error: r.Error, Activity: r.Activity, Slice: len(r.Slice)}
	s.TotalLines = r.PR.Additions + r.PR.Deletions
	s.Pushed = r.ReadSHA != "" && r.ReadSHA != r.PR.HeadSHA
	if b := r.Brief; b != nil {
		s.Decisions = len(b.Decisions)
		for _, d := range b.Decisions {
			if a, ok := r.Answers[d.ID]; ok && a.Option != "" {
				s.Answered++
			}
		}
		total := 0
		for _, sec := range b.Sections {
			total += sec.Lines
			if sec.Kind == model.Judgment {
				s.NeedLines += sec.Lines
				s.Minutes += sec.Minutes
			} else if sec.Kind == model.Skim {
				s.Minutes += sec.Minutes
			}
		}
		if total > 0 {
			s.TotalLines = total
		}
		for _, c := range b.Claims {
			if c.Status == model.Contradicted {
				s.Contra++
			}
		}
		s.HasContext = b.Context != nil
	}
	switch {
	case r.Status == "posted":
		s.Lane = "done"
	case r.Status == "failed":
		s.Lane = "failed"
	case r.Status != "ready":
		s.Lane = "reading"
	case s.Decisions > 0:
		s.Lane = "judgment"
	case s.NeedLines > 0:
		s.Lane = "skim"
	default:
		s.Lane = "clear"
	}
	return s
}

// List returns the queue, most pressing first.
func (e *Engine) List() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := Status{Viewer: e.viewer, SyncErr: e.syncErr, LastSync: e.lastSync}
	for _, r := range e.reviews {
		st.Reviews = append(st.Reviews, summarize(r))
	}
	rank := map[string]int{"judgment": 0, "skim": 1, "clear": 2, "reading": 3, "failed": 4, "done": 5}
	sort.Slice(st.Reviews, func(i, j int) bool {
		a, b := st.Reviews[i], st.Reviews[j]
		if rank[a.Lane] != rank[b.Lane] {
			return rank[a.Lane] < rank[b.Lane]
		}
		if a.Contra != b.Contra {
			return a.Contra > b.Contra
		}
		if a.Decisions != b.Decisions {
			return a.Decisions > b.Decisions
		}
		return a.PR.UpdatedAt.After(b.PR.UpdatedAt)
	})
	return st
}

// FileDiff is part of one file's diff, ready to draw.
type FileDiff struct {
	Path   string      `json:"path"`
	Status string      `json:"status"`
	Binary bool        `json:"binary,omitempty"`
	Hunks  []diff.Hunk `json:"hunks"`
}

// View is one review with its diffs cut to what each section and decision
// points at.
type View struct {
	model.Review
	Key      string                `json:"key"`
	Summary  Summary               `json:"summary"`
	Sections map[string][]FileDiff `json:"sectionDiffs"`
	Hot      map[string][]FileDiff `json:"hotDiffs"`
	Rules    map[string]model.Rule `json:"rules"`
	// Areas is how well the reviewer knows each part this PR touches.
	Areas  []AreaSeen `json:"areas"`
	OutDir string     `json:"outDir"`
}

type AreaSeen struct {
	Area string `json:"area"`
	Seen
	Touched bool `json:"touched"`
}

func (e *Engine) files(r *model.Review) []diff.File {
	sha := r.ReadSHA
	if sha == "" {
		return nil
	}
	ck := r.PR.Key() + "@" + sha
	e.mu.Lock()
	f, ok := e.diffs[ck]
	e.mu.Unlock()
	if ok {
		return f
	}
	pr := r.PR
	pr.HeadSHA = sha
	b, err := os.ReadFile(filepath.Join(repo.Layout(e.Home, pr).Dir, "diff.patch"))
	if err != nil {
		return nil
	}
	f = diff.Parse(string(b))
	e.mu.Lock()
	e.diffs[ck] = f
	e.mu.Unlock()
	return f
}

func (e *Engine) get(key string) (*model.Review, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, ok := e.reviews[key]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

// Get returns one review with its diffs.
func (e *Engine) Get(key string) (*View, bool) {
	r, ok := e.get(key)
	if !ok {
		return nil, false
	}
	v := &View{Review: *r, Key: key, Summary: summarize(r), Sections: map[string][]FileDiff{}, Hot: map[string][]FileDiff{}, Rules: map[string]model.Rule{}}
	pr := r.PR
	if r.ReadSHA != "" {
		pr.HeadSHA = r.ReadSHA
	}
	v.OutDir = repo.Layout(e.Home, pr).Out
	files := e.files(r)
	var touched []string
	for _, f := range files {
		touched = append(touched, f.Path)
	}
	if b := r.Brief; b != nil {
		for _, s := range b.Sections {
			for _, fr := range s.Files {
				if f, ok := diff.Find(files, fr.Path); ok {
					v.Sections[s.ID] = append(v.Sections[s.ID], FileDiff{Path: f.Path, Status: f.Status, Binary: f.Binary, Hunks: diff.Slice(f, fr.Ranges, 3)})
				}
			}
		}
		for _, d := range b.Decisions {
			for _, fr := range d.Hot {
				if f, ok := diff.Find(files, fr.Path); ok {
					v.Hot[d.ID] = append(v.Hot[d.ID], FileDiff{Path: f.Path, Status: f.Status, Hunks: diff.Mark(diff.Slice(f, fr.Ranges, 4), fr.Ranges)})
				}
			}
			if d.Rule != "" {
				if rule, ok := e.Rules.Get(d.Rule); ok {
					v.Rules[d.Rule] = rule
				}
			}
		}
		for _, h := range b.RuleHits {
			if rule, ok := e.Rules.Get(h.Rule); ok {
				v.Rules[h.Rule] = rule
			}
		}
	}
	m := loadMap(e.Home)[r.PR.Owner+"/"+r.PR.Repo]
	for _, a := range Areas(touched) {
		v.Areas = append(v.Areas, AreaSeen{Area: a, Seen: m[a], Touched: true})
	}
	return v, true
}

// SetAnswer records the reviewer's choice on a decision.
func (e *Engine) SetAnswer(key, decision string, a model.Answer) error {
	e.mu.Lock()
	r := e.reviews[key]
	if r == nil || r.Brief == nil {
		e.mu.Unlock()
		return errors.New("no such review")
	}
	var d *model.Decision
	for i := range r.Brief.Decisions {
		if r.Brief.Decisions[i].ID == decision {
			d = &r.Brief.Decisions[i]
		}
	}
	if d == nil {
		e.mu.Unlock()
		return errors.New("no such decision")
	}
	prev := r.Answers[decision]
	a.At = time.Now()
	// The reviewer acting on a rule's finding is itself a verdict on the
	// rule, until they say otherwise.
	if a.Feedback == "" {
		a.Feedback = prev.Feedback
	}
	if a.Feedback == "" && d.Rule != "" && d.Rule != "repo" && a.Option != "" {
		a.Feedback = "useful"
	}
	if a.Option == "" {
		delete(r.Answers, decision)
	} else {
		r.Answers[decision] = a
	}
	if r.Status == "posted" {
		r.Status = "ready"
	}
	rule := d.Rule
	repoName := r.PR.Owner + "/" + r.PR.Repo
	e.save(r)
	e.mu.Unlock()

	if rule != "" && rule != "repo" {
		e.Rules.Record(rule, prev.Feedback, a.Feedback)
		if a.Feedback != prev.Feedback {
			e.applyFeedback(rule, repoName, a.Feedback)
		}
	}
	return nil
}

// applyFeedback changes the rule itself when the reviewer says it should not
// have stopped them: "not here" exempts this repository, "stop" retires it.
func (e *Engine) applyFeedback(id, repoName, fb string) {
	rule, ok := e.Rules.Get(id)
	if !ok {
		return
	}
	switch fb {
	case "not-here":
		if rule.Scope == "everywhere" && !contains(rule.Except, repoName) {
			rule.Except = append(rule.Except, repoName)
			_, _ = e.Rules.Save(rule, "you said it does not hold in "+repoName)
		}
	case "stop":
		if rule.Status != "retired" {
			rule.Status = "retired"
			_, _ = e.Rules.Save(rule, "retired: you said to stop raising it")
		}
	}
}

// AddTime adds active seconds against a section or decision.
func (e *Engine) AddTime(key, id string, seconds int) {
	if seconds <= 0 || seconds > 600 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.reviews[key]
	if r == nil {
		return
	}
	r.Seconds[id] += seconds
	// Time is saved without waking the UI: it shows on the next fetch.
	_ = config.WriteJSON(e.statePath(r.PR), r)
}

// Audit marks a mechanical or skim section as spot-checked by the reviewer.
func (e *Engine) Audit(key, section string, done bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.reviews[key]
	if r == nil {
		return
	}
	if done {
		r.Audited[section] = true
	} else {
		delete(r.Audited, section)
	}
	e.save(r)
}

// SetNotes replaces the reviewer's free comments.
func (e *Engine) SetNotes(key string, notes []model.Note) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.reviews[key]
	if r == nil {
		return
	}
	r.Notes = notes
	e.save(r)
}
