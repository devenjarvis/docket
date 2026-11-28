package review_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/demo"
	"github.com/devenjarvis/docket/internal/ledger"
	"github.com/devenjarvis/docket/internal/lens"
	"github.com/devenjarvis/docket/internal/review"
)

func withStrategy(strategy, instructions string) func(*review.Engine, string) {
	return func(eng *review.Engine, home string) {
		ls, err := lens.Open(home)
		if err != nil {
			panic(err)
		}
		eng.Lenses = ls
		eng.Strategy = func(string) string { return strategy }
		eng.Instructions = func() string { return instructions }
		eng.Ledger = ledger.Open(home)
	}
}

func readEntry(t *testing.T, home string, number int) ledger.Entry {
	t.Helper()
	all, err := ledger.Open(home).All()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range all {
		if e.Kind == ledger.KindRead && e.Number == number {
			return e
		}
	}
	t.Fatalf("no read of #%d in the ledger", number)
	return ledger.Entry{}
}

func TestAPanelOfLensesFeedsTheLeadReader(t *testing.T) {
	eng, home := start(t, withStrategy(config.StrategyPanel, "Call out anything that touches billing."))
	const key = "acme/payments-api#4127"
	v := waitReady(t, eng, key)

	if v.Strategy != config.StrategyPanel {
		t.Errorf("strategy = %q, want panel", v.Strategy)
	}
	on, _ := eng.Lenses.Active()
	if len(v.Lenses) != len(on) || len(on) == 0 {
		t.Fatalf("lens runs = %d, want one per lens that is on (%d)", len(v.Lenses), len(on))
	}
	leads, kept := 0, 0
	for _, r := range v.Lenses {
		if r.Failed {
			t.Errorf("lens %s failed: %s", r.ID, r.Error)
		}
		// Lenses run on the agent's smallest model; the lead stays on its own.
		if r.Model != demo.DemoSmallModel {
			t.Errorf("lens %s ran on %q, want the small model", r.ID, r.Model)
		}
		leads += r.Leads
		kept += r.Kept
	}
	if leads == 0 || kept != len(v.Brief.Decisions) {
		t.Errorf("leads=%d kept=%d decisions=%d: every decision should credit a lens", leads, kept, len(v.Brief.Decisions))
	}
	if v.Model == demo.DemoSmallModel {
		t.Error("the lead reader ran on the small model under the panel strategy")
	}
	dir := filepath.Dir(v.OutDir)
	b, err := os.ReadFile(filepath.Join(dir, "lenses.json"))
	if err != nil || !strings.Contains(string(b), `"correctness"`) {
		t.Errorf("lenses.json = %s (%v)", b, err)
	}
	e := readEntry(t, home, 4127)
	if e.Strategy != config.StrategyPanel || len(e.Lenses) != len(on) {
		t.Errorf("ledger entry: strategy=%q lenses=%d", e.Strategy, len(e.Lenses))
	}
	if len(e.Usage) < 2 {
		t.Errorf("usage should be split by model (lead and lenses), got %+v", e.Usage)
	}
}

func TestASwarmReadsEverythingOnTheSmallModel(t *testing.T) {
	eng, _ := start(t, withStrategy(config.StrategySwarm, ""))
	v := waitReady(t, eng, "acme/payments-api#4127")
	if v.Model != demo.DemoSmallModel {
		t.Errorf("lead model = %q, want the small model", v.Model)
	}
	if len(v.Lenses) == 0 {
		t.Error("no lens ran")
	}
}

func TestAPanelWithNoLensOnReadsAlone(t *testing.T) {
	eng, _ := start(t, withStrategy(config.StrategyPanel, ""), func(eng *review.Engine, _ string) {
		all, _ := eng.Lenses.List()
		for _, l := range all {
			l.On = false
			if _, err := eng.Lenses.Save(l); err != nil {
				panic(err)
			}
		}
	})
	v := waitReady(t, eng, "acme/payments-api#4127")
	if v.Strategy != config.StrategySingle || len(v.Lenses) != 0 {
		t.Errorf("strategy=%q lenses=%d, want a single read", v.Strategy, len(v.Lenses))
	}
}

func TestTheCriticRemovesWhatDoesNotHold(t *testing.T) {
	eng, home := start(t, withStrategy(config.StrategyCritic, ""))
	v := waitReady(t, eng, "acme/payments-api#4127")
	if v.Critic == nil || v.Critic.Failed {
		t.Fatalf("critic = %+v", v.Critic)
	}
	if len(v.Critic.Dropped) != 1 || len(v.Brief.Decisions) != 2 {
		t.Errorf("dropped=%v decisions=%d, want one of three removed", v.Critic.Dropped, len(v.Brief.Decisions))
	}
	logged := false
	for _, w := range v.Brief.Work {
		if strings.HasPrefix(w.Did, "Critic removed") {
			logged = true
		}
	}
	if !logged {
		t.Error("the critic's removal is not in the work log")
	}
	if _, err := os.Stat(filepath.Join(v.OutDir, "first-brief.json")); err != nil {
		t.Errorf("the first brief was not kept: %v", err)
	}
	if e := readEntry(t, home, 4127); e.Dropped != 1 || e.Strategy != config.StrategyCritic {
		t.Errorf("ledger: dropped=%d strategy=%q", e.Dropped, e.Strategy)
	}
}
