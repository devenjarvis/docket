package ledger

import (
	"math"
	"testing"
	"time"

	"github.com/devenjarvis/docket/internal/acp"
	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/model"
)

func TestAddAllClear(t *testing.T) {
	l := Open(t.TempDir())
	if all, err := l.All(); err != nil || len(all) != 0 {
		t.Fatalf("empty ledger: %v %v", all, err)
	}
	if err := l.Add(Entry{Kind: KindPost, Repo: "a/b", Number: 1, Event: "APPROVE"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Add(Entry{Kind: KindRead, Repo: "a/b", Number: 1}); err != nil {
		t.Fatal(err)
	}
	all, err := l.All()
	if err != nil || len(all) != 2 || all[0].At.IsZero() || all[1].Kind != KindRead {
		t.Fatalf("All = %+v, %v", all, err)
	}
	if err := l.Clear(); err != nil {
		t.Fatal(err)
	}
	if all, _ := l.All(); len(all) != 0 {
		t.Fatalf("after Clear: %v", all)
	}
}

func TestSummarize(t *testing.T) {
	to := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -30)
	day := func(n int) time.Time { return to.AddDate(0, 0, -n) }
	tok := func(model string, in, out int64) acp.Usage {
		return acp.Usage{Model: model, Turns: 1, Tokens: acp.Tokens{Input: in, Output: out, Total: in + out}}
	}
	all := []Entry{
		{At: day(40), Kind: KindPost, Event: "APPROVE"}, // the period before
		{At: day(20), Kind: KindRead, Seconds: 200, Tries: 2, Proved: 3, Contradicted: 1, Isolation: "enforced",
			Usage: []acp.Usage{{Model: "Big", Turns: 2, Tokens: acp.Tokens{Input: 1000, Output: 100, Total: 1100}, Cost: 1.5, Currency: "USD", HasCost: true}}},
		{At: day(19), Kind: KindAsk, Usage: []acp.Usage{tok("Priced", 1_000_000, 1_000_000)}},
		{At: day(18), Kind: KindTend, Auto: true, Usage: []acp.Usage{tok("Nobody", 500, 5)}},
		{At: day(18), Kind: KindStep, Outcome: StepApproved, Floor: true},
		{At: day(18), Kind: KindStep, Outcome: StepAuto},
		{At: day(10), Kind: KindPost, Event: "APPROVE", YourSeconds: 300, NeedLines: 40, Time: map[string]int{"brief": 100, "decisions": 200}},
		{At: day(5), Kind: KindPost, Event: "REQUEST_CHANGES", YourSeconds: 100, Receipt: true},
	}
	prices := map[string]config.Price{"priced": {Input: 3, Output: 15}}
	r := Summarize(all, from, to, prices)
	if r.Signed != 2 || r.SignedBefore != 1 || r.Verdicts["APPROVE"] != 1 || r.Receipts != 1 {
		t.Fatalf("signed %d before %d verdicts %v", r.Signed, r.SignedBefore, r.Verdicts)
	}
	if r.YourMedian != 200 || r.YourTotal != 400 || r.TimeBy["decisions"] != 200 {
		t.Fatalf("your time %d %d %v", r.YourMedian, r.YourTotal, r.TimeBy)
	}
	if r.Reads != 1 || r.SentBack != 1 || r.Enforced != 1 || r.Contradicted != 1 {
		t.Fatalf("reads %+v", r)
	}
	if r.Tend.Runs != 1 || r.Tend.AutoRuns != 1 || r.Tend.Floor != 1 || r.Tend.Outcomes[StepAuto] != 1 {
		t.Fatalf("tend %+v", r.Tend)
	}
	c := r.Cost
	if math.Abs(c.Reported-1.5) > 1e-9 || math.Abs(c.Estimated-18) > 1e-9 || math.Abs(c.Total-19.5) > 1e-9 {
		t.Fatalf("cost reported %v estimated %v total %v", c.Reported, c.Estimated, c.Total)
	}
	if len(c.Unpriced) != 1 || c.Unpriced[0] != "Nobody" {
		t.Fatalf("unpriced %v", c.Unpriced)
	}
	if c.ByJob[JobReading].Cost != 1.5 || c.ByJob[JobMine].Runs != 1 || c.Runs != 3 || c.Reporting != 3 {
		t.Fatalf("by job %+v runs %d", c.ByJob, c.Runs)
	}
	if math.Abs(c.PerSigned-(1.5+18)/2) > 1e-9 {
		t.Fatalf("per signed %v", c.PerSigned)
	}
	if len(c.ByModel) != 3 || c.ByModel[0].Model != "Priced" {
		t.Fatalf("by model %+v", c.ByModel)
	}
	if r.Unit != "week" || len(r.Buckets) < 4 {
		t.Fatalf("unit %s buckets %d", r.Unit, len(r.Buckets))
	}
	n := 0
	for _, b := range r.Buckets {
		n += b.Verdicts["APPROVE"] + b.Verdicts["REQUEST_CHANGES"]
	}
	if n != 2 {
		t.Fatalf("bucketed verdicts %d", n)
	}
}

func TestSummarizeComparesStrategiesAndLenses(t *testing.T) {
	to := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	at := to.AddDate(0, 0, -1)
	cost := func(c float64) []acp.Usage { return []acp.Usage{{Model: "m", HasCost: true, Cost: c, Currency: "USD"}} }
	all := []Entry{
		// Before strategies existed: counted as single.
		{At: at, Kind: KindRead, Seconds: 100, Decisions: 2, Usage: cost(1)},
		{At: at, Kind: KindRead, Strategy: config.StrategyPanel, Seconds: 300, Decisions: 3, Usage: cost(0.5), Lenses: []model.LensRun{
			{ID: "security", Name: "Security", Leads: 2, Kept: 1},
			{ID: "tests", Name: "Tests", Leads: 3, Kept: 2},
			{ID: "claims", Name: "Description against code", Failed: true},
		}},
		{At: at, Kind: KindRead, Strategy: config.StrategyCritic, Failed: true},
		{At: at, Kind: KindRead, Strategy: config.StrategyCritic, Seconds: 200, Decisions: 1, Dropped: 2},
		{At: at, Kind: KindPost, Strategy: config.StrategyPanel, Decisions: 3, Blocking: 1,
			LensDecisions: map[string]int{"security": 1, "tests": 2}, LensBlocking: map[string]int{"security": 1}},
	}
	r := Summarize(all, to.AddDate(0, 0, -7), to, nil)
	if len(r.Strategies) != 3 {
		t.Fatalf("strategies = %+v", r.Strategies)
	}
	single, panel, critic := r.Strategies[0], r.Strategies[1], r.Strategies[2]
	if single.Strategy != config.StrategySingle || single.Reads != 1 || single.Cost != 1 || single.Decisions != 2 {
		t.Errorf("single = %+v", single)
	}
	if panel.Strategy != config.StrategyPanel || panel.Signed != 1 || panel.Blocking != 1 || panel.SignedDecisions != 3 || panel.ReadMedian != 300 || panel.Cost != 0.5 {
		t.Errorf("panel = %+v", panel)
	}
	if critic.Reads != 2 || critic.Failed != 1 || critic.Dropped != 2 || critic.ReadMedian != 200 {
		t.Errorf("critic = %+v", critic)
	}
	byID := map[string]LensRow{}
	for _, l := range r.Lenses {
		byID[l.ID] = l
	}
	if l := byID["tests"]; l.Leads != 3 || l.Kept != 2 || l.Signed != 2 || l.Blocking != 0 || r.Lenses[0].ID != "tests" {
		t.Errorf("tests lens = %+v (first: %s)", l, r.Lenses[0].ID)
	}
	if l := byID["security"]; l.Blocking != 1 || l.Name != "Security" {
		t.Errorf("security lens = %+v", l)
	}
	if l := byID["claims"]; l.Failed != 1 || l.Runs != 1 {
		t.Errorf("claims lens = %+v", l)
	}
}
