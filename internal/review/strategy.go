package review

import (
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
	"github.com/devenjarvis/docket/internal/lens"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/repo"
)

// panelWidth is how many lenses run at once on one agent process.
const panelWidth = 4

// lensTimeout bounds one lens. A lens is meant to be quick; one that is not
// is dropped rather than holding up the read.
const lensTimeout = 12 * time.Minute

func (e *Engine) strategyIn(repo string) string {
	if e.Strategy == nil {
		return config.StrategySingle
	}
	if s := e.Strategy(repo); config.ValidStrategy(s) {
		return s
	}
	return config.StrategySingle
}

func (e *Engine) instructions() string {
	if e.Instructions == nil {
		return ""
	}
	return strings.TrimSpace(e.Instructions())
}

func (e *Engine) activeLenses() []lens.Lens {
	if e.Lenses == nil {
		return nil
	}
	on, err := e.Lenses.Active()
	if err != nil {
		e.Logf("could not read the lenses: %v", err)
	}
	return on
}

// useSmallest puts session on the agent's smallest model. It returns false
// when the agent offers none or would not switch, and the session stays on
// its default.
func useSmallest(ctx context.Context, client *acp.Client, session string) bool {
	id, ok := acp.Smallest(client.Models())
	if !ok {
		return false
	}
	return client.SetModel(ctx, session, id) == nil
}

// lensReport is one lens's findings as the lead reader gets them.
type lensReport struct {
	Lens     string       `json:"lens"`
	Name     string       `json:"name"`
	Findings []brief.Lead `json:"findings"`
}

// runPanel runs each lens in its own read-only session on client, on the
// agent's smallest model, a few at a time, and writes what they found to
// lenses.json for the lead reader. It returns how each lens did and the ids
// of the lenses that finished. A lens that fails is recorded and skipped:
// the panel is there to widen the read, not to gate it.
func (e *Engine) runPanel(ctx context.Context, key string, client *acp.Client, ws repo.Workspace, lenses []lens.Lens, self, slice bool) ([]model.LensRun, map[string]bool, error) {
	runs := make([]model.LensRun, len(lenses))
	reports := make([]*lensReport, len(lenses))
	instructions := e.instructions()
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		done int
	)
	sem := make(chan struct{}, panelWidth)
	e.setActivity(key, fmt.Sprintf("Running %d lenses", len(lenses)))
	for i, l := range lenses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				runs[i] = model.LensRun{ID: l.ID, Name: l.Name, Failed: true, Error: "the read stopped first"}
				return
			}
			defer func() { <-sem }()
			run, leads := e.runLens(ctx, client, ws, l, self, slice, instructions)
			runs[i] = run
			if !run.Failed {
				reports[i] = &lensReport{Lens: l.ID, Name: l.Name, Findings: leads}
			}
			mu.Lock()
			done++
			n := done
			mu.Unlock()
			e.setActivity(key, fmt.Sprintf("Lenses: %d of %d done (%s: %s)", n, len(lenses), l.Name, lensOutcome(run)))
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return runs, nil, ctx.Err()
	}
	ran := map[string]bool{}
	out := []lensReport{}
	for _, r := range reports {
		if r != nil {
			ran[r.Lens] = true
			out = append(out, *r)
		}
	}
	if err := config.WriteJSON(filepath.Join(ws.Dir, "lenses.json"), out); err != nil {
		return runs, nil, err
	}
	return runs, ran, nil
}

func lensOutcome(r model.LensRun) string {
	switch {
	case r.Failed:
		return "failed"
	case r.Leads == 0:
		return "nothing"
	case r.Leads == 1:
		return "1 lead"
	}
	return fmt.Sprintf("%d leads", r.Leads)
}

// runLens runs one lens and reads back its findings.
func (e *Engine) runLens(parent context.Context, client *acp.Client, ws repo.Workspace, l lens.Lens, self, slice bool, instructions string) (model.LensRun, []brief.Lead) {
	run := model.LensRun{ID: l.ID, Name: l.Name}
	started := time.Now()
	ctx, cancel := context.WithTimeout(parent, lensTimeout)
	defer cancel()
	failed := func(err error) (model.LensRun, []brief.Lead) {
		run.Failed, run.Error = true, err.Error()
		run.Seconds = int(time.Since(started).Seconds())
		e.Logf("lens %s on %s: %v", l.ID, ws.Dir, err)
		return run, nil
	}
	dir := filepath.Join(ws.Dir, "lenses", l.ID)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return failed(err)
	}
	file := filepath.Join(dir, "findings.json")
	session, err := client.NewSession(ctx, ws.Head)
	if err != nil {
		return failed(fmt.Errorf("would not open a session: %w", err))
	}
	// A lens reads everything the read was given and writes only its own
	// file. It runs nothing: several lenses share the checkouts at once.
	client.SetPolicy(session, &acp.Policy{Job: "lens", Read: []string{ws.Dir}, Write: []string{dir}})
	useSmallest(ctx, client, session)
	run.Model = client.SessionModel(session)
	h := &handler{runCode: false}
	prompt := brief.LensPrompt(brief.LensData{Workspace: ws, Name: l.Name, Text: l.Text, File: file, Self: self, Slice: slice, Instructions: instructions})
	if _, err := client.Prompt(ctx, session, prompt, h); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil {
			return failed(fmt.Errorf("took longer than %s", lensTimeout))
		}
		return failed(err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return failed(errors.New("wrote no findings"))
	}
	leads, err := brief.ParseLeads(raw)
	if err != nil {
		return failed(err)
	}
	run.Leads = len(leads)
	run.Seconds = int(time.Since(started).Seconds())
	return run, leads
}

// keptByLens fills in how many of each lens's leads became decisions.
func keptByLens(runs []model.LensRun, b *model.Brief) {
	n := map[string]int{}
	for _, d := range b.Decisions {
		if d.Lens != "" {
			n[d.Lens]++
		}
	}
	for i := range runs {
		runs[i].Kept = n[runs[i].ID]
	}
}

// runCritic has a fresh session of the agent's model try to knock down each
// decision and claim in b, and rewrite brief.json without the ones that
// fall. When the critic cannot produce a brief that validates, the first one
// stands and the critic run says why.
func (e *Engine) runCritic(ctx context.Context, key string, client *acp.Client, ws repo.Workspace, files []diff.File, ruleIDs, lensIDs map[string]bool, b *model.Brief, runCode, self bool) (*model.Brief, *model.CriticRun) {
	cr := &model.CriticRun{}
	briefPath := filepath.Join(ws.Out, "brief.json")
	first, err := os.ReadFile(briefPath)
	if err != nil {
		cr.Failed, cr.Error = true, err.Error()
		return b, cr
	}
	firstPath := filepath.Join(ws.Out, "first-brief.json")
	keepFirst := func(err error) (*model.Brief, *model.CriticRun) {
		_ = os.WriteFile(briefPath, first, 0o644)
		cr.Failed, cr.Error = true, err.Error()
		e.Logf("%s: the critic could not finish, so the first brief stands: %v", key, err)
		return b, cr
	}
	if err := os.WriteFile(firstPath, first, 0o644); err != nil {
		return keepFirst(err)
	}
	e.setActivity(key, "The critic is checking the brief")
	session, err := client.NewSession(ctx, ws.Head)
	if err != nil {
		return keepFirst(fmt.Errorf("would not open a session: %w", err))
	}
	client.SetPolicy(session, acp.ReviewPolicy(ws.Dir, ws.Head, ws.Base, ws.Scratch, ws.Out))
	h := &handler{runCode: runCode, onActivity: func(s string) { e.setActivity(key, "Critic: "+s) }}
	prompt := brief.CriticPrompt(brief.CriticData{Workspace: ws, RunCode: runCode, Self: self, Instructions: e.instructions()})
	var nb *model.Brief
	for attempt := 0; ; attempt++ {
		if _, err := client.Prompt(ctx, session, prompt, h); err != nil {
			if ctx.Err() != nil {
				_ = os.WriteFile(briefPath, first, 0o644)
				return b, nil
			}
			return keepFirst(err)
		}
		var problems []string
		raw, rerr := os.ReadFile(briefPath)
		switch {
		case rerr != nil:
			problems = []string{"there is no file at " + briefPath}
		default:
			nb, err = brief.Parse(raw)
			if err != nil {
				problems = []string{err.Error()}
			} else {
				problems = append(brief.Validate(nb, files, ruleIDs), brief.ValidateLenses(nb, lensIDs)...)
			}
		}
		if len(problems) == 0 {
			break
		}
		if attempt >= maxRetries {
			return keepFirst(fmt.Errorf("its brief failed validation after %d tries: %s", attempt+1, strings.Join(problems, "; ")))
		}
		prompt = brief.RetryPrompt(ws.Out, problems)
	}
	kept := map[string]bool{}
	for _, d := range nb.Decisions {
		kept[d.ID] = true
	}
	for _, d := range b.Decisions {
		if !kept[d.ID] {
			cr.Dropped = append(cr.Dropped, d.Title)
		}
	}
	was := map[string]string{}
	for _, c := range b.Claims {
		was[c.ID] = c.Status
	}
	for _, c := range nb.Claims {
		if s, ok := was[c.ID]; ok && s != c.Status {
			cr.Changed++
		}
	}
	return nb, cr
}
