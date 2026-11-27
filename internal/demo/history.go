package demo

import (
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/devenjarvis/docket/internal/acp"
	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/ledger"
)

// seedHistory writes three months of made-up history to the ledger, so the
// demo's Settings has metrics to show, and prices the scripted agent's small
// model, which reports tokens but no cost. It leaves a ledger that already
// has entries alone.
func seedHistory(home string) {
	l := ledger.Open(home)
	if all, _ := l.All(); len(all) > 0 {
		return
	}
	if _, err := os.Stat(filepath.Join(home, "config.json")); os.IsNotExist(err) {
		_ = config.WriteJSON(filepath.Join(home, "config.json"), config.Config{
			Prices: map[string]config.Price{DemoSmallModel: {Input: 1, Output: 5}},
		})
	}
	rnd := rand.New(rand.NewSource(4127))
	repos := []string{"acme/payments-api", "acme/ledger", "acme/web-app", "acme/billing-sync"}
	const agent, big = "Scripted demo agent", "Scripted demo agent"
	usage := func(model string, in, out, cache int64, cost float64) []acp.Usage {
		u := acp.Usage{Model: model, Turns: 1, Tokens: acp.Tokens{Input: in, Output: out, Total: in + out, CacheRead: cache}}
		if cost > 0 {
			u.Cost, u.Currency, u.HasCost = cost, "USD", true
		}
		return []acp.Usage{u}
	}
	jitter := func(base float64) float64 { return base * (0.6 + rnd.Float64()*0.9) }
	var out []ledger.Entry
	n := 900
	for d := 90; d >= 1; d-- {
		day := now.AddDate(0, 0, -d)
		if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}
		reads := rnd.Intn(3)
		for i := 0; i < reads; i++ {
			n++
			repo := repos[rnd.Intn(len(repos))]
			at := day.Add(time.Duration(9+rnd.Intn(8))*time.Hour + time.Duration(rnd.Intn(60))*time.Minute)
			tries := 1
			if rnd.Intn(8) == 0 {
				tries = 2
			}
			total := 20 + rnd.Intn(380)
			need := total * (5 + rnd.Intn(30)) / 100
			decisions := rnd.Intn(4)
			mech := rnd.Intn(4)
			reread := rnd.Intn(4) == 0
			failed := rnd.Intn(18) == 0
			in := int64(jitter(52000)) * int64(tries)
			e := ledger.Entry{At: at, Kind: ledger.KindRead, Repo: repo, Number: n, Agent: agent, Model: big,
				Seconds: 120 + rnd.Intn(420), Tries: tries, Reread: reread, Failed: failed,
				Usage:     usage(big, in, int64(jitter(9000)), int64(jitter(140000)), float64(in)/52000*jitter(0.84)),
				Isolation: "enforced", NeedLines: need, TotalLines: total, Decisions: decisions, Mechanical: mech}
			if reread {
				e.Carried = rnd.Intn(decisions + 1)
			}
			if !failed {
				e.Proved, e.Contradicted, e.Unproven = 2+rnd.Intn(6), boolInt(rnd.Intn(5) == 0), rnd.Intn(3)
			}
			if rnd.Intn(15) == 0 {
				e.Isolation = "soft"
			}
			out = append(out, e)
			if failed {
				continue
			}
			asks := 0
			if rnd.Intn(3) == 0 {
				asks = 1
				out = append(out, ledger.Entry{At: at.Add(20 * time.Minute), Kind: ledger.KindAsk, Repo: repo, Number: n, Model: big,
					Seconds: 30 + rnd.Intn(90), Usage: usage(big, int64(jitter(12000)), int64(jitter(1400)), int64(jitter(30000)), jitter(0.16))})
			}
			// Most reads end in a signed review a little later.
			if rnd.Intn(6) == 0 {
				continue
			}
			minutes := 1 + need/12 + rnd.Intn(4) + decisions*2
			secs := minutes * 60
			event := "APPROVE"
			blocking := 0
			switch r := rnd.Intn(10); {
			case r < 2 && decisions > 0:
				event, blocking = "REQUEST_CHANGES", 1
			case r < 4:
				event = "COMMENT"
			}
			dec := secs * (40 + rnd.Intn(20)) / 100
			if decisions == 0 {
				dec = 0
			}
			brief := secs * 15 / 100
			ctx := 0
			if rnd.Intn(4) == 0 {
				ctx = secs * 10 / 100
			}
			out = append(out, ledger.Entry{At: at.Add(time.Duration(40+rnd.Intn(200)) * time.Minute), Kind: ledger.KindPost, Repo: repo, Number: n,
				Event: event, Agent: agent, Model: big, NeedLines: need, TotalLines: total, Decisions: decisions, Mechanical: mech,
				YourSeconds: secs, Time: map[string]int{"decisions": dec, "brief": brief, "context": ctx, "diff": secs - dec - brief - ctx},
				Answered: decisions, Edited: rnd.Intn(decisions + 1), Blocking: blocking, Asks: asks, Audited: rnd.Intn(mech + 1), Receipt: rnd.Intn(10) > 0})
		}
		// The agent on the user's own pull requests, a few times a week.
		if rnd.Intn(3) == 0 {
			n++
			at := day.Add(time.Duration(10+rnd.Intn(7)) * time.Hour)
			repo := repos[rnd.Intn(2)]
			steps := 1 + rnd.Intn(4)
			gate := usage(DemoSmallModel, int64(steps)*int64(jitter(4100)), int64(steps)*200, 0, 0)
			out = append(out, ledger.Entry{At: at, Kind: ledger.KindTend, Repo: repo, Number: n, Agent: agent, Model: big,
				Seconds: 90 + rnd.Intn(300), Auto: rnd.Intn(3) > 0, Steps: steps, Stopped: rnd.Intn(25) == 0,
				Usage: append(usage(big, int64(jitter(34000)), int64(jitter(6500)), int64(jitter(90000)), jitter(0.52)), gate...)})
			kinds := []string{"fix", "reply", "resolve", "rerun"}
			for i := 0; i < steps; i++ {
				outcome, floor := ledger.StepAuto, false
				switch r := rnd.Intn(10); {
				case r < 2:
					outcome, floor = ledger.StepApproved, rnd.Intn(2) == 0
				case r < 3:
					outcome, floor = ledger.StepRejected, rnd.Intn(2) == 0
				}
				out = append(out, ledger.Entry{At: at.Add(time.Duration(i+1) * time.Minute), Kind: ledger.KindStep, Repo: repo, Number: n,
					Step: kinds[rnd.Intn(len(kinds))], Outcome: outcome, Floor: floor})
				if outcome == ledger.StepAuto && rnd.Intn(14) == 0 {
					out = append(out, ledger.Entry{At: at.Add(3 * time.Hour), Kind: ledger.KindStep, Repo: repo, Number: n, Step: "fix", Outcome: ledger.StepUndone})
				}
			}
		}
	}
	for _, e := range out {
		_ = l.Add(e)
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
