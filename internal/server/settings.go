package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/acp"
	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/ledger"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/review"
)

// The Settings page's routes: metrics, the per-repository policy, what is
// on disk, the log, and a check that the agent starts.
func (s *Server) settingsRoutes() {
	s.mux.HandleFunc("GET /api/metrics", s.metrics)
	s.mux.HandleFunc("GET /api/repos", s.repos)
	s.mux.HandleFunc("POST /api/repo", s.setRepo)
	s.mux.HandleFunc("GET /api/data", s.data)
	s.mux.HandleFunc("GET /api/logtail", s.logTail)
	s.mux.HandleFunc("POST /api/sweep", s.sweep)
	s.mux.HandleFunc("GET /api/ledger", s.ledgerRaw)
	s.mux.HandleFunc("POST /api/ledger/clear", s.ledgerClear)
	s.mux.HandleFunc("POST /api/agent/check", s.checkAgent)
}

func (s *Server) ledger() *ledger.Ledger {
	if s.Eng.Ledger != nil {
		return s.Eng.Ledger
	}
	return ledger.Open(s.Home)
}

// metrics is the report for the last `days` days (0: everything), with the
// rulebook's counts and the familiarity map, which have their own stores.
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	all, err := s.ledger().All()
	if err != nil {
		fail(w, 500, err)
		return
	}
	c, _ := config.Read()
	to := time.Now()
	var from time.Time
	if days > 0 {
		from = to.AddDate(0, 0, -days)
	}
	rep := ledger.Summarize(all, from, to, c.Prices)
	ruleList, _ := s.Rules.List()
	type ruleRow struct {
		ID      string          `json:"id"`
		Text    string          `json:"text"`
		Status  string          `json:"status"`
		Stats   model.RuleStats `json:"stats"`
		Fading  bool            `json:"fading"`
		Applies string          `json:"applies"`
	}
	rows := []ruleRow{}
	for _, rl := range ruleList {
		if rl.Status == "retired" {
			continue
		}
		rows = append(rows, ruleRow{rl.ID, rl.Text, rl.Status, rl.Stats, rl.Fading(), rl.Applies})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Stats.Fired > rows[j].Stats.Fired })
	writeJSON(w, map[string]any{
		"report": rep,
		"rules":  rows,
		"map":    review.LoadMap(s.Home),
		"prices": c.Prices,
	})
}

// RepoRow is one repository in Settings' per-repository table.
type RepoRow struct {
	Repo    string `json:"repo"`
	Off     bool   `json:"off"`
	RunCode string `json:"runCode"` // default, on or off
	Tend    string `json:"tend"`    // default, auto, ask or off
	Self    string `json:"self"`    // default, auto, ask or off
	Reviews int    `json:"reviews"` // reviews posted, from the ledger
	Open    int    `json:"open"`    // in the queue or among yours now
}

// knownRepos lists every repository Docket has met: in the queue, among the
// user's own pull requests, in the familiarity map, in the ledger, and any
// the user set something for.
func (s *Server) knownRepos() []RepoRow {
	c, _ := config.Read()
	rows := map[string]*RepoRow{}
	row := func(repo string) *RepoRow {
		if repo == "" {
			return nil
		}
		r := rows[repo]
		if r == nil {
			r = &RepoRow{Repo: repo, RunCode: "default", Tend: "default", Self: "default"}
			rows[repo] = r
		}
		return r
	}
	for _, q := range s.Eng.List().Reviews {
		row(q.PR.Owner+"/"+q.PR.Repo).Open++
	}
	for _, m := range s.Eng.Mine() {
		row(m.PR.Owner+"/"+m.PR.Repo).Open++
	}
	for repo := range review.LoadMap(s.Home) {
		row(repo)
	}
	if all, err := s.ledger().All(); err == nil {
		for _, e := range all {
			if r := row(e.Repo); r != nil && e.Kind == ledger.KindPost {
				r.Reviews++
			}
		}
	}
	for repo, rc := range c.Repos {
		r := row(repo)
		r.Off = rc.Off
		if rc.RunCode != nil {
			r.RunCode = map[bool]string{true: "on", false: "off"}[*rc.RunCode]
		}
		if rc.Tend != "" {
			r.Tend = rc.Tend
		}
		if rc.SelfReview != "" {
			r.Self = rc.SelfReview
		}
	}
	out := make([]RepoRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Reviews+out[i].Open != out[j].Reviews+out[j].Open {
			return out[i].Reviews+out[i].Open > out[j].Reviews+out[j].Open
		}
		return out[i].Repo < out[j].Repo
	})
	return out
}

func (s *Server) repos(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"repos": s.knownRepos()})
}

// setRepo changes one repository's settings. A setting left out is kept;
// "default" clears an override.
func (s *Server) setRepo(w http.ResponseWriter, r *http.Request) {
	in, ok := read[struct {
		Repo    string  `json:"repo"`
		Off     *bool   `json:"off"`
		RunCode *string `json:"runCode"`
		Tend    *string `json:"tend"`
		Self    *string `json:"self"`
	}](w, r)
	if !ok {
		return
	}
	repo := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(in.Repo, "https://github.com/"), "/"))
	if parts := strings.Split(repo, "/"); len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		fail(w, 400, errors.New("name a repository as owner/name"))
		return
	}
	c, _ := config.Read()
	if c.Repos == nil {
		c.Repos = map[string]config.Repo{}
	}
	rc := c.Repos[repo]
	if in.Off != nil {
		rc.Off = *in.Off
	}
	if in.RunCode != nil {
		switch *in.RunCode {
		case "default":
			rc.RunCode = nil
		case "on", "off":
			v := *in.RunCode == "on"
			rc.RunCode = &v
		default:
			fail(w, 400, errors.New("runCode must be default, on or off"))
			return
		}
	}
	if in.Tend != nil {
		switch *in.Tend {
		case "default":
			rc.Tend = ""
		case "auto", "ask", "off":
			rc.Tend = *in.Tend
		default:
			fail(w, 400, errors.New("tend must be default, auto, ask or off"))
			return
		}
	}
	if in.Self != nil {
		switch *in.Self {
		case "default":
			rc.SelfReview = ""
		case "auto", "ask", "off":
			rc.SelfReview = *in.Self
		default:
			fail(w, 400, errors.New("self must be default, auto, ask or off"))
			return
		}
	}
	if rc == (config.Repo{}) {
		delete(c.Repos, repo)
	} else {
		c.Repos[repo] = rc
	}
	if err := config.Write(c); err != nil {
		fail(w, 500, err)
		return
	}
	if in.Off != nil || in.Self != nil {
		// Turning a repository off empties it from the queue now, and
		// turning it on finds its pull requests now, not at the next poll.
		s.resync()
	}
	writeJSON(w, map[string]any{"ok": true, "repos": s.knownRepos()})
}

// dirSize adds up the sizes of the files under p. Unreadable parts are
// skipped.
func dirSize(p string) int64 {
	var n int64
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// data is what Docket keeps on disk, by part.
func (s *Server) data(w http.ResponseWriter, _ *http.Request) {
	type part struct {
		Path  string `json:"path"`
		What  string `json:"what"`
		Bytes int64  `json:"bytes"`
	}
	parts := []part{
		{"reviews", "Review checkouts and the agent's probes", 0},
		{"tend", "Checkouts of your branches", 0},
		{"repos", "Bare clones", 0},
		{"state", "Reviews and your pull requests in progress", 0},
		{"rules", "Rulebook, with its history", 0},
		{"ledger.jsonl", "Metrics ledger", 0},
		{"map.json", "What you know of each repository", 0},
		{"config.json", "Settings", 0},
		{"docket.log", "Log", 0},
	}
	var total int64
	for i := range parts {
		parts[i].Bytes = dirSize(filepath.Join(s.Home, parts[i].Path))
		total += parts[i].Bytes
	}
	writeJSON(w, map[string]any{"home": s.Home, "parts": parts, "total": total})
}

// logTail is the last lines of docket.log.
func (s *Server) logTail(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 || n > 2000 {
		n = 200
	}
	b, err := os.ReadFile(filepath.Join(s.Home, "docket.log"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fail(w, 500, err)
		return
	}
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	if lines == nil {
		lines = []string{}
	}
	writeJSON(w, map[string]any{"lines": lines})
}

func (s *Server) sweep(w http.ResponseWriter, _ *http.Request) {
	before := dirSize(filepath.Join(s.Home, "reviews"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	s.Eng.Sweep(ctx)
	after := dirSize(filepath.Join(s.Home, "reviews"))
	writeJSON(w, map[string]any{"freed": max(before-after, 0)})
}

func (s *Server) ledgerRaw(w http.ResponseWriter, _ *http.Request) {
	b, err := s.ledger().Raw()
	if err != nil {
		fail(w, 500, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="docket-ledger.jsonl"`)
	_, _ = w.Write(b)
}

func (s *Server) ledgerClear(w http.ResponseWriter, _ *http.Request) {
	if err := s.ledger().Clear(); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// checkAgent starts the agent, opens a session in an empty folder and stops
// it, so a broken install shows in Settings rather than on a pull request.
func (s *Server) checkAgent(w http.ResponseWriter, r *http.Request) {
	command, name := s.AgentCommand()
	if command == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "No agent is set up."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "docket-check-")
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer os.RemoveAll(dir)
	started := time.Now()
	var stderr bytes.Buffer
	res := map[string]any{"name": name, "command": command}
	client, err := acp.Start(ctx, command, dir, &stderr)
	if err != nil {
		res["ok"], res["error"] = false, fmt.Sprintf("%v%s", err, tailText(stderr.String()))
		writeJSON(w, res)
		return
	}
	defer client.Close()
	if _, err := client.NewSession(ctx, dir); err != nil {
		res["ok"], res["error"] = false, fmt.Sprintf("it started but would not open a session: %v%s", err, tailText(stderr.String()))
		writeJSON(w, res)
		return
	}
	models := []string{}
	for _, m := range client.Models() {
		if m.Name != "" {
			models = append(models, m.Name)
		} else {
			models = append(models, m.ID)
		}
	}
	res["ok"], res["seconds"], res["model"], res["models"] = true, time.Since(started).Seconds(), client.Model(), models
	res["protocol"] = acp.ProtocolVersion
	writeJSON(w, res)
}

func tailText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) > 400 {
		s = "…" + s[len(s)-400:]
	}
	return ": " + s
}
