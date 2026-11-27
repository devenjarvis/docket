package ledger

import (
	"sort"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/acp"
	"github.com/devenjarvis/docket/internal/config"
)

// Jobs the agent's cost is split by.
const (
	JobReading   = "reading"   // reads of pull requests to review
	JobQuestions = "questions" // follow-up questions from Your call
	JobMine      = "mine"      // planning and judging work on the user's own pull requests
)

func jobOf(kind string) string {
	switch kind {
	case KindRead:
		return JobReading
	case KindAsk:
		return JobQuestions
	case KindTend:
		return JobMine
	}
	return ""
}

// Report is what Settings shows for one period.
type Report struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Since is the first entry in the ledger, so the page can say what
	// "all" covers; zero when the ledger is empty.
	Since time.Time `json:"since"`
	Empty bool      `json:"empty"`

	Signed       int            `json:"signed"`
	SignedBefore int            `json:"signedBefore"` // the period of equal length before From
	Verdicts     map[string]int `json:"verdicts"`
	// Your active time, in seconds.
	YourMedian int            `json:"yourMedian"`
	YourTotal  int            `json:"yourTotal"`
	TimeBy     map[string]int `json:"timeBy"`
	Points     []Point        `json:"points"`

	NeedLines  int `json:"needLines"`
	TotalLines int `json:"totalLines"`
	Answered   int `json:"answered"`
	Edited     int `json:"edited"`
	Blocking   int `json:"blocking"`
	Asks       int `json:"asks"`
	Carried    int `json:"carried"`
	Receipts   int `json:"receipts"`
	Audited    int `json:"audited"`
	Mechanical int `json:"mechanical"`

	Reads        int `json:"reads"`
	ReadsFailed  int `json:"readsFailed"`
	Rereads      int `json:"rereads"`
	SentBack     int `json:"sentBack"` // briefs the validator returned
	Enforced     int `json:"enforced"` // reads that kept to Docket's file policy
	ReadMedian   int `json:"readMedian"`
	Proved       int `json:"proved"`
	Contradicted int `json:"contradicted"`
	Unproven     int `json:"unproven"`

	Tend    TendReport `json:"tend"`
	Cost    CostReport `json:"cost"`
	Buckets []Bucket   `json:"buckets"`
	// Unit is what one bucket spans: day, week or month.
	Unit string `json:"unit"`
}

// Point is one posted review: the lines that needed the reviewer against the
// time they spent.
type Point struct {
	Repo    string `json:"repo"`
	Number  int    `json:"number"`
	Need    int    `json:"need"`
	Seconds int    `json:"seconds"`
}

type TendReport struct {
	Runs     int            `json:"runs"`
	AutoRuns int            `json:"autoRuns"`
	Failed   int            `json:"failed"`
	Stopped  int            `json:"stopped"`
	Outcomes map[string]int `json:"outcomes"`
	// Held steps, by what held them.
	Floor  int `json:"floor"`
	Judged int `json:"judged"`
}

// Bucket is one day, week or month of the period.
type Bucket struct {
	Start    time.Time          `json:"start"`
	Verdicts map[string]int     `json:"verdicts"`
	Cost     map[string]float64 `json:"cost"`   // by job
	Tokens   map[string]int64   `json:"tokens"` // by job
}

type CostReport struct {
	// Total is Reported plus Estimated, in US dollars.
	Total     float64 `json:"total"`
	Reported  float64 `json:"reported"`
	Estimated float64 `json:"estimated"`
	// Tokens is every token reported, priced or not.
	Tokens acp.Tokens `json:"tokens"`
	// Unpriced lists models that reported tokens but no cost and have no
	// price in Settings, so their tokens add nothing to Total.
	Unpriced []string `json:"unpriced"`
	// Other lists currencies other than USD the agent reported, which are
	// left out of Total.
	Other   []string           `json:"other"`
	ByJob   map[string]JobCost `json:"byJob"`
	ByModel []ModelCost        `json:"byModel"`
	// PerSigned is the reading and questions cost divided by reviews
	// signed; PerRun the cost of one run on the user's own pull requests.
	PerSigned float64 `json:"perSigned"`
	PerRun    float64 `json:"perRun"`
	// Reporting counts the runs that reported any usage, of Runs.
	Reporting int `json:"reporting"`
	Runs      int `json:"runs"`
}

type JobCost struct {
	Cost   float64 `json:"cost"`
	Tokens int64   `json:"tokens"`
	Runs   int     `json:"runs"`
}

type ModelCost struct {
	Model string `json:"model"`
	acp.Tokens
	Runs      int     `json:"runs"`
	Turns     int     `json:"turns"`
	Reported  float64 `json:"reported"`
	Estimated float64 `json:"estimated"`
	Priced    bool    `json:"priced"`
}

// PriceFor finds a model's price: the exact name, else the same name in
// another case.
func PriceFor(prices map[string]config.Price, model string) (config.Price, bool) {
	if p, ok := prices[model]; ok {
		return p, true
	}
	for k, p := range prices {
		if strings.EqualFold(k, model) {
			return p, true
		}
	}
	return config.Price{}, false
}

// Estimate prices tokens at p, in dollars. Input tokens are taken to
// exclude cached reads and writes, which have prices of their own.
func Estimate(t acp.Tokens, p config.Price) float64 {
	return (float64(t.Input)*p.Input + float64(t.Output+t.Thought)*p.Output +
		float64(t.CacheRead)*p.CacheRead + float64(t.CacheWrite)*p.CacheWrite) / 1e6
}

// costOf is what one usage cost: the agent's own figure when it gave one in
// dollars, else an estimate from prices. ok is false when neither is known.
func costOf(u acp.Usage, prices map[string]config.Price) (reported, estimated float64, ok bool) {
	if u.HasCost && (u.Currency == "" || strings.EqualFold(u.Currency, "USD")) {
		return u.Cost, 0, true
	}
	if u.HasCost {
		return 0, 0, true
	}
	if p, found := PriceFor(prices, u.Model); found && u.Turns > 0 {
		return 0, Estimate(u.Tokens, p), true
	}
	return 0, 0, u.Turns == 0
}

func addTokens(a *acp.Tokens, b acp.Tokens) {
	a.Total += b.Total
	a.Input += b.Input
	a.Output += b.Output
	a.Thought += b.Thought
	a.CacheRead += b.CacheRead
	a.CacheWrite += b.CacheWrite
}

func median(v []int) int {
	if len(v) == 0 {
		return 0
	}
	s := append([]int(nil), v...)
	sort.Ints(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// Summarize builds the report for entries between from and to. A zero from
// means since the first entry.
func Summarize(all []Entry, from, to time.Time, prices map[string]config.Price) Report {
	r := Report{From: from, To: to, Verdicts: map[string]int{}, TimeBy: map[string]int{}, Points: []Point{}}
	r.Tend.Outcomes = map[string]int{}
	r.Cost.ByJob = map[string]JobCost{}
	r.Cost.Unpriced, r.Cost.Other = []string{}, []string{}
	for _, e := range all {
		if r.Since.IsZero() || e.At.Before(r.Since) {
			r.Since = e.At
		}
	}
	r.Empty = len(all) == 0
	if from.IsZero() {
		from = r.Since
		if from.IsZero() {
			from = to
		}
		r.From = from
	}
	span := to.Sub(from)
	r.Unit = "week"
	switch {
	case span <= 15*24*time.Hour:
		r.Unit = "day"
	case span > 200*24*time.Hour:
		r.Unit = "month"
	}
	r.Buckets = buckets(from, to, r.Unit)

	before := from.Add(-span)
	var yours, reads []int
	unpriced, other := map[string]bool{}, map[string]bool{}
	byModel := map[string]*ModelCost{}
	for _, e := range all {
		if e.Kind == KindPost && !e.At.Before(before) && e.At.Before(from) {
			r.SignedBefore++
		}
		if e.At.Before(from) || e.At.After(to) {
			continue
		}
		b := bucketFor(r.Buckets, e.At)
		switch e.Kind {
		case KindPost:
			r.Signed++
			r.Verdicts[e.Event]++
			if b != nil {
				b.Verdicts[e.Event]++
			}
			yours = append(yours, e.YourSeconds)
			r.YourTotal += e.YourSeconds
			for k, v := range e.Time {
				r.TimeBy[k] += v
			}
			r.Points = append(r.Points, Point{Repo: e.Repo, Number: e.Number, Need: e.NeedLines, Seconds: e.YourSeconds})
			r.NeedLines += e.NeedLines
			r.TotalLines += e.TotalLines
			r.Answered += e.Answered
			r.Edited += e.Edited
			r.Blocking += e.Blocking
			r.Asks += e.Asks
			r.Audited += e.Audited
			r.Mechanical += e.Mechanical
			if e.Receipt {
				r.Receipts++
			}
		case KindRead:
			r.Reads++
			if e.Failed {
				r.ReadsFailed++
			} else {
				reads = append(reads, e.Seconds)
			}
			if e.Reread {
				r.Rereads++
				r.Carried += e.Carried
			}
			if e.Tries > 1 {
				r.SentBack += e.Tries - 1
			}
			if e.Isolation == "enforced" {
				r.Enforced++
			}
			r.Proved += e.Proved
			r.Contradicted += e.Contradicted
			r.Unproven += e.Unproven
		case KindTend:
			r.Tend.Runs++
			if e.Auto {
				r.Tend.AutoRuns++
			}
			if e.Failed {
				r.Tend.Failed++
			}
			if e.Stopped {
				r.Tend.Stopped++
			}
		case KindStep:
			r.Tend.Outcomes[e.Outcome]++
			if e.Outcome == StepApproved || e.Outcome == StepRejected {
				if e.Floor {
					r.Tend.Floor++
				} else {
					r.Tend.Judged++
				}
			}
		}

		job := jobOf(e.Kind)
		if job == "" {
			continue
		}
		r.Cost.Runs++
		if len(e.Usage) > 0 {
			r.Cost.Reporting++
		}
		jc := r.Cost.ByJob[job]
		jc.Runs++
		for _, u := range e.Usage {
			rep, est, ok := costOf(u, prices)
			if u.HasCost && rep == 0 && est == 0 && u.Currency != "" && !strings.EqualFold(u.Currency, "USD") {
				other[u.Currency] = true
			}
			if !ok {
				unpriced[u.Model] = true
			}
			r.Cost.Reported += rep
			r.Cost.Estimated += est
			addTokens(&r.Cost.Tokens, u.Tokens)
			jc.Cost += rep + est
			jc.Tokens += u.Total
			if b != nil {
				b.Cost[job] += rep + est
				b.Tokens[job] += u.Total
			}
			m := byModel[u.Model]
			if m == nil {
				_, priced := PriceFor(prices, u.Model)
				m = &ModelCost{Model: u.Model, Priced: priced}
				byModel[u.Model] = m
			}
			m.Runs++
			m.Turns += u.Turns
			addTokens(&m.Tokens, u.Tokens)
			m.Reported += rep
			m.Estimated += est
		}
		r.Cost.ByJob[job] = jc
	}
	r.YourMedian = median(yours)
	r.ReadMedian = median(reads)
	r.Cost.Total = r.Cost.Reported + r.Cost.Estimated
	for m := range unpriced {
		r.Cost.Unpriced = append(r.Cost.Unpriced, m)
	}
	sort.Strings(r.Cost.Unpriced)
	for c := range other {
		r.Cost.Other = append(r.Cost.Other, c)
	}
	sort.Strings(r.Cost.Other)
	for _, m := range byModel {
		r.Cost.ByModel = append(r.Cost.ByModel, *m)
	}
	sort.Slice(r.Cost.ByModel, func(i, j int) bool {
		a, b := r.Cost.ByModel[i], r.Cost.ByModel[j]
		if a.Reported+a.Estimated != b.Reported+b.Estimated {
			return a.Reported+a.Estimated > b.Reported+b.Estimated
		}
		return a.Total > b.Total
	})
	if r.Cost.ByModel == nil {
		r.Cost.ByModel = []ModelCost{}
	}
	if r.Signed > 0 {
		r.Cost.PerSigned = (r.Cost.ByJob[JobReading].Cost + r.Cost.ByJob[JobQuestions].Cost) / float64(r.Signed)
	}
	if n := r.Cost.ByJob[JobMine].Runs; n > 0 {
		r.Cost.PerRun = r.Cost.ByJob[JobMine].Cost / float64(n)
	}
	return r
}

// buckets cuts from..to into days, Monday weeks or calendar months, in
// to's location.
func buckets(from, to time.Time, unit string) []Bucket {
	loc := to.Location()
	d := from.In(loc)
	start := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	switch unit {
	case "week":
		start = start.AddDate(0, 0, -((int(start.Weekday()) + 6) % 7))
	case "month":
		start = time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, loc)
	}
	var out []Bucket
	for t := start; !t.After(to) && len(out) < 400; t = next(t, unit) {
		out = append(out, Bucket{Start: t, Verdicts: map[string]int{}, Cost: map[string]float64{}, Tokens: map[string]int64{}})
	}
	return out
}

func next(t time.Time, unit string) time.Time {
	switch unit {
	case "day":
		return t.AddDate(0, 0, 1)
	case "month":
		return t.AddDate(0, 1, 0)
	}
	return t.AddDate(0, 0, 7)
}

func bucketFor(bs []Bucket, at time.Time) *Bucket {
	for i := len(bs) - 1; i >= 0; i-- {
		if !at.Before(bs[i].Start) {
			return &bs[i]
		}
	}
	return nil
}
