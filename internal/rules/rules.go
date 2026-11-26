// Package rules is the reviewer's rulebook: one plain-text file per rule
// under ~/.docket/rules, with the counts of how often each fired and whether
// the reviewer agreed kept beside them. The directory is a git repository, so
// every change to what the reviewer enforces has a history and can be undone.
package rules

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/model"
)

type Store struct {
	dir string
	mu  sync.Mutex
}

// Open returns the rulebook under home, creating it if needed.
func Open(home string) (*Store, error) {
	dir := filepath.Join(home, "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir}
	if _, err := os.Stat(filepath.Join(dir, ".git")); errors.Is(err, os.ErrNotExist) {
		s.git("init", "-q")
	}
	return s, nil
}

func (s *Store) Dir() string { return s.dir }

// git runs best-effort: the rulebook works without git, it just has no
// history.
func (s *Store) git(args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Dir = s.dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Docket", "GIT_AUTHOR_EMAIL=docket@localhost",
		"GIT_COMMITTER_NAME=Docket", "GIT_COMMITTER_EMAIL=docket@localhost")
	_ = cmd.Run()
}

func (s *Store) commit(msg string) {
	s.git("add", "-A")
	s.git("commit", "-q", "-m", msg)
}

func (s *Store) statsPath() string { return filepath.Join(s.dir, "stats.json") }

func (s *Store) readStats() map[string]model.RuleStats {
	out := map[string]model.RuleStats{}
	b, err := os.ReadFile(s.statsPath())
	if err == nil {
		_ = jsonUnmarshal(b, &out)
	}
	return out
}

// List returns every rule, retired ones included, ordered by id.
func (s *Store) List() ([]model.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list()
}

func (s *Store) list() ([]model.Rule, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	stats := s.readStats()
	var out []model.Rule
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		r, ok := parse(string(b))
		if !ok {
			continue
		}
		r.Stats = stats[r.ID]
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return idNum(out[i].ID) < idNum(out[j].ID) })
	return out, nil
}

func idNum(id string) int {
	n, _ := strconv.Atoi(strings.TrimLeft(id, "R"))
	return n
}

// ForRepo returns the active and trial rules that hold in repo (owner/name).
func (s *Store) ForRepo(repo string) ([]model.Rule, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []model.Rule
	for _, r := range all {
		if r.Status == "retired" {
			continue
		}
		if r.Scope != "everywhere" && !strings.EqualFold(r.Scope, repo) {
			continue
		}
		skip := false
		for _, e := range r.Except {
			if strings.EqualFold(e, repo) {
				skip = true
			}
		}
		if !skip {
			out = append(out, r)
		}
	}
	return out, nil
}

// Save writes a rule, giving it the next id when it has none, and records
// why in the rulebook's history.
func (s *Store) Save(r model.Rule, why string) (model.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Text = strings.TrimSpace(r.Text)
	if r.Text == "" {
		return r, errors.New("a rule needs its sentence")
	}
	if r.ID == "" {
		all, err := s.list()
		if err != nil {
			return r, err
		}
		n := 0
		for _, o := range all {
			n = max(n, idNum(o.ID))
		}
		r.ID = fmt.Sprintf("R%02d", n+1)
	}
	if r.Scope == "" {
		r.Scope = "everywhere"
	}
	if r.Severity == "" {
		r.Severity = "note"
	}
	if r.Status == "" {
		r.Status = "active"
	}
	if r.Created.IsZero() {
		r.Created = time.Now()
	}
	if err := os.WriteFile(filepath.Join(s.dir, r.ID+".md"), []byte(format(r)), 0o644); err != nil {
		return r, err
	}
	if why == "" {
		why = "edited"
	}
	s.commit(r.ID + ": " + why)
	r.Stats = s.readStats()[r.ID]
	return r, nil
}

// Get returns one rule.
func (s *Store) Get(id string) (model.Rule, bool) {
	all, _ := s.List()
	for _, r := range all {
		if r.ID == id {
			return r, true
		}
	}
	return model.Rule{}, false
}

// Record counts one firing of a rule that the reviewer has now judged.
// prev is the reviewer's earlier verdict on the same finding ("" for none),
// so changing an answer moves the count instead of adding to it.
func (s *Store) Record(id, prev, now string) {
	if id == "" || prev == now {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := s.readStats()
	st := stats[id]
	apply := func(v string, d int) {
		switch v {
		case "useful":
			st.Agreed += d
		case "not-here", "stop":
			st.Overrode += d
		default:
			return
		}
		st.Fired += d
	}
	apply(prev, -1)
	apply(now, 1)
	stats[id] = st
	_ = config.WriteJSON(s.statsPath(), stats)
	s.commit(id + ": counted a firing")
}

// --- file format -----------------------------------------------------------
//
//	---
//	id: R07
//	scope: everywhere
//	applies: HTTP clients
//	severity: blocking
//	status: active
//	origin: Written by you
//	created: 2026-03-02
//	except: acme/web, acme/infra
//	---
//	Retries of non-idempotent requests must carry an idempotency key.

func format(r model.Rule) string {
	var b strings.Builder
	b.WriteString("---\n")
	w := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s: %s\n", k, strings.ReplaceAll(v, "\n", " "))
		}
	}
	w("id", r.ID)
	w("scope", r.Scope)
	w("applies", r.Applies)
	w("severity", r.Severity)
	w("status", r.Status)
	w("origin", r.Origin)
	w("created", r.Created.Format("2006-01-02"))
	w("except", strings.Join(r.Except, ", "))
	b.WriteString("---\n")
	b.WriteString(r.Text)
	b.WriteString("\n")
	return b.String()
}

func parse(s string) (model.Rule, bool) {
	var r model.Rule
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return r, false
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return r, false
	}
	for _, ln := range strings.Split(rest[:end], "\n") {
		k, v, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "id":
			r.ID = v
		case "scope":
			r.Scope = v
		case "applies":
			r.Applies = v
		case "severity":
			r.Severity = v
		case "status":
			r.Status = v
		case "origin":
			r.Origin = v
		case "created":
			r.Created, _ = time.Parse("2006-01-02", v)
		case "except":
			for _, e := range strings.Split(v, ",") {
				if e = strings.TrimSpace(e); e != "" {
					r.Except = append(r.Except, e)
				}
			}
		}
	}
	body := rest[end+4:]
	body = strings.TrimPrefix(body, "\n")
	r.Text = strings.TrimSpace(body)
	return r, r.ID != "" && r.Text != ""
}

// History returns the last n changes to the rulebook, newest first.
func (s *Store) History(n int) []string {
	cmd := exec.Command("git", "log", fmt.Sprintf("-%d", n), "--date=format:%b %-d", "--pretty=format:%ad · %s")
	cmd.Dir = s.dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" && !strings.HasSuffix(l, "counted a firing") {
			lines = append(lines, l)
		}
	}
	return lines
}
