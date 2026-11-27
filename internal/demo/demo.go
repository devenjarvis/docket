// Package demo runs Docket with no GitHub account and no real agent: three
// sample pull requests, a scripted agent that speaks ACP, and a seeded
// rulebook. It exists so the whole loop can be seen and tested end to end.
package demo

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/gh"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/repo"
	"github.com/devenjarvis/docket/internal/rules"
)

//go:embed fixtures
var fixtures embed.FS

func fixture(name string) string {
	b, err := fixtures.ReadFile("fixtures/" + name)
	if err != nil {
		return ""
	}
	return string(b)
}

var now = time.Now()

var prs = []model.PR{
	{Owner: "acme", Repo: "payments-api", Number: 4127, Title: "Retry webhook deliveries with exponential backoff", Author: "maya-r",
		Body:    "Failed webhook deliveries are now retried up to 5 times with exponential backoff (1 s, 2 s, 4 s, 8 s).\n\n- Nothing changes for a delivery that succeeds first time.\n- Receivers will not see duplicate events.\n- Adds an `attempts` column; the migration is safe to run with no downtime.\n- Renames `send` to `deliver`.",
		HeadSHA: "9f2c1d7a40b1e6c3a2d5", BaseSHA: "1b7e0c4d92aa", BaseRef: "main", HeadRef: "maya/webhook-retry",
		Additions: 96, Deletions: 31, Files: 9, UpdatedAt: now.Add(-40 * time.Minute), Why: []string{"review requested"}},
	{Owner: "acme", Repo: "ledger", Number: 311, Title: "Round settlement amounts per currency minor unit", Author: "sam-t",
		Body:    "Settlement amounts were truncated to the minor unit, which dropped fractions of a cent. They are now rounded.\n\nRounding uses banker's rounding, as the settlement spec requires.",
		HeadSHA: "c41a9e0b77d2f3a1", BaseSHA: "77aa01", BaseRef: "main", HeadRef: "sam/rounding",
		Additions: 6, Deletions: 1, Files: 2, UpdatedAt: now.Add(-3 * time.Hour), Why: []string{"review requested", "assigned"}},
	{Owner: "acme", Repo: "web-app", Number: 2214, Title: "Fix date picker off-by-one in UTC-negative zones", Author: "priya-n",
		Body:    "Fixes the off-by-one day for users in UTC-negative time zones. No change for users at or east of UTC.",
		HeadSHA: "5d0e3b1c9a8f7e6d", BaseSHA: "aa91", BaseRef: "main", HeadRef: "priya/datepicker-tz",
		Additions: 14, Deletions: 2, Files: 2, UpdatedAt: now.Add(-26 * time.Hour), Why: []string{"team: web-reviewers"}},
}

// GitHub is a gh.Client over the sample pull requests. Posted reviews are
// written to <home>/posted so a demo run can be inspected.
type GitHub struct{ Home string }

func (GitHub) Viewer(context.Context) (string, error) { return "devenjarvis", nil }

func (GitHub) Assigned(context.Context) ([]model.PR, error) {
	out := make([]model.PR, len(prs))
	copy(out, prs)
	return out, nil
}

func (g GitHub) PR(_ context.Context, owner, name string, n int) (model.PR, error) {
	in := g.state()
	in.mu.Lock()
	o := in.find(owner, name, n)
	var own model.PR
	if o != nil {
		own = o.pr
	}
	in.mu.Unlock()
	if o != nil {
		own.URL = fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, name, n)
		return own, nil
	}
	for _, p := range prs {
		if p.Owner == owner && p.Repo == name && p.Number == n {
			p.URL = fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, name, n)
			return p, nil
		}
	}
	return model.PR{}, fmt.Errorf("the demo has no pull request %s", model.Key(owner, name, n))
}

func (g GitHub) PostReview(_ context.Context, pr model.PR, in gh.ReviewInput) (string, error) {
	p := filepath.Join(g.Home, "posted", fmt.Sprintf("%s-%d.json", pr.Repo, pr.Number))
	if err := config.WriteJSON(p, in); err != nil {
		return "", err
	}
	return fmt.Sprintf("https://github.com/%s/%s/pull/%d#pullrequestreview-demo", pr.Owner, pr.Repo, pr.Number), nil
}

// Checkout is a repo.Preparer that makes the directories and returns the
// sample diff, with no git involved.
type Checkout struct{ Home string }

func (c Checkout) Prepare(_ context.Context, pr model.PR) (repo.Workspace, string, error) {
	ws := repo.Layout(c.Home, pr)
	for _, d := range []string{ws.Head, ws.Base, ws.Scratch, ws.Out} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return ws, "", err
		}
	}
	d := fixture(fmt.Sprintf("%d.patch", pr.Number))
	if d == "" {
		return ws, "", fmt.Errorf("the demo has no diff for #%d", pr.Number)
	}
	return ws, d, nil
}

func (Checkout) Release(context.Context, model.PR, repo.Workspace) {}

// Seed fills an empty rulebook and reviewer map with believable history, so
// the rulebook screen and the context page have something to show.
func Seed(home string, rs *rules.Store) {
	seedHistory(home)
	if all, _ := rs.List(); len(all) > 0 {
		return
	}
	day := func(months int) time.Time { return now.AddDate(0, -months, 0) }
	seed := []model.Rule{
		{ID: "R03", Text: "New exported functions have a doc comment.", Applies: "Go and TypeScript", Scope: "everywhere", Severity: "note", Status: "active", Origin: "Written by you", Created: day(11)},
		{ID: "R07", Text: "Retries of non-idempotent requests must carry an idempotency key.", Applies: "HTTP clients", Scope: "everywhere", Severity: "blocking", Status: "active", Origin: "Started in acme/ledger, widened after you said it in acme/billing-sync", Created: day(8)},
		{ID: "R09", Text: "Money is rounded with banker's rounding.", Applies: "amounts and currency", Scope: "acme/ledger", Severity: "blocking", Status: "active", Origin: "Written by you", Created: day(7)},
		{ID: "R12", Text: "A NOT NULL column added without a default needs a stated deploy order.", Applies: "SQL migrations", Scope: "everywhere", Severity: "question", Status: "active", Origin: "Written by you", Created: day(6)},
		{ID: "R15", Text: "Outbound HTTP calls set an explicit timeout.", Applies: "HTTP clients", Scope: "acme/payments-api", Severity: "note", Status: "trial", Origin: "Drafted from a decision on acme/payments-api #4098", Created: day(1)},
	}
	for _, r := range seed {
		_, _ = rs.Save(r, "seeded for the demo")
	}
	_ = config.WriteJSON(filepath.Join(rs.Dir(), "stats.json"), map[string]model.RuleStats{
		"R03": {Fired: 41, Agreed: 9, Overrode: 32},
		"R07": {Fired: 14, Agreed: 11, Overrode: 3},
		"R09": {Fired: 4, Agreed: 4},
		"R12": {Fired: 6, Agreed: 6},
		"R15": {Fired: 1, Agreed: 1},
	})
	_ = config.WriteJSON(filepath.Join(home, "map.json"), map[string]map[string]any{
		"acme/payments-api": {
			"src/delivery": map[string]any{"reviews": 2, "last": day(5)},
			"src/api":      map[string]any{"reviews": 3, "last": day(11)},
		},
		"acme/ledger": {
			"src/outbox":     map[string]any{"reviews": 9, "last": day(1)},
			"src/settlement": map[string]any{"reviews": 14, "last": now.AddDate(0, 0, -9)},
		},
	})
}

// IsDemoPrompt reports whether an agent prompt came from Docket's reading or
// asking prompts, and pulls the directories out of it.
func dirsFromPrompt(prompt string) (dir, out string) {
	for _, ln := range strings.Split(prompt, "\n") {
		if i := strings.Index(ln, "/diff.patch"); i >= 0 && dir == "" {
			j := strings.LastIndexAny(ln[:i], "` ")
			dir = ln[j+1 : i]
		}
		if i := strings.Index(ln, "/brief.json"); i >= 0 && out == "" {
			j := strings.LastIndexAny(ln[:i], "` ")
			out = ln[j+1 : i]
		}
	}
	return dir, out
}
