package server_test

import (
	"strings"
	"testing"

	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/lens"
)

func TestStrategySettings(t *testing.T) {
	ts := serve(t)
	if code, body := postJSON(t, ts, "/api/settings", map[string]any{"strategy": "panel", "instructions": "  Flag new SQL.  "}); code != 200 {
		t.Fatalf("set strategy: %d %s", code, body)
	}
	c, _ := config.Read()
	if c.StrategyMode() != config.StrategyPanel || c.Instructions != "Flag new SQL." {
		t.Errorf("config = %+v", c)
	}
	if code, _ := postJSON(t, ts, "/api/settings", map[string]any{"strategy": "mob"}); code != 400 {
		t.Errorf("an unknown strategy was accepted: %d", code)
	}
	if code, _ := postJSON(t, ts, "/api/settings", map[string]any{"instructions": strings.Repeat("x", config.MaxInstructions+1)}); code != 400 {
		t.Errorf("instructions over the limit were accepted: %d", code)
	}
	if code, body := postJSON(t, ts, "/api/repo", map[string]any{"repo": "acme/web", "strategy": "critic"}); code != 200 {
		t.Fatalf("set repo strategy: %d %s", code, body)
	}
	c, _ = config.Read()
	if c.StrategyIn("acme/web") != config.StrategyCritic || c.StrategyIn("acme/other") != config.StrategyPanel {
		t.Errorf("per-repository strategy: %+v", c.Repos)
	}
	postJSON(t, ts, "/api/repo", map[string]any{"repo": "acme/web", "strategy": "default"})
	if c, _ = config.Read(); c.Repos["acme/web"].Strategy != "" {
		t.Errorf("default did not clear the override: %+v", c.Repos)
	}
}

func TestLensRoutes(t *testing.T) {
	ts := serve(t)
	type list struct {
		Lenses  []lens.Lens `json:"lenses"`
		Missing int         `json:"missingBuiltins"`
	}
	var l list
	if getJSON(t, ts, "/api/lenses", &l); len(l.Lenses) != len(lens.Defaults()) || l.Missing != 0 {
		t.Fatalf("lenses = %d, missing %d", len(l.Lenses), l.Missing)
	}
	if code, body := postJSON(t, ts, "/api/lens", map[string]any{"id": "security", "on": false}); code != 200 || !strings.Contains(body, `"saved":"security"`) {
		t.Errorf("turn off: %d %s", code, body)
	}
	if code, body := postJSON(t, ts, "/api/lens", map[string]any{"name": "Migrations", "text": "Check every migration can be rolled back."}); code != 200 {
		t.Errorf("add: %d %s", code, body)
	}
	if code, _ := postJSON(t, ts, "/api/lens", map[string]any{"name": "Migrations", "text": "again"}); code != 400 {
		t.Errorf("a second lens with the same name was accepted: %d", code)
	}
	if code, _ := postJSON(t, ts, "/api/lens", map[string]any{"id": "nope", "on": true}); code != 404 {
		t.Errorf("changed a lens that does not exist: %d", code)
	}
	postJSON(t, ts, "/api/lens", map[string]any{"id": "tests", "text": "Only integration tests."})
	postJSON(t, ts, "/api/lens/reset", map[string]any{"id": "tests"})
	postJSON(t, ts, "/api/lens/delete", map[string]any{"id": "claims"})
	getJSON(t, ts, "/api/lenses", &l)
	by := map[string]lens.Lens{}
	for _, x := range l.Lenses {
		by[x.ID] = x
	}
	if by["security"].On || by["migrations"].Text == "" || by["tests"].Edited || l.Missing != 1 {
		t.Errorf("after changes: %+v missing=%d", by, l.Missing)
	}
	postJSON(t, ts, "/api/lenses/restore", nil)
	if getJSON(t, ts, "/api/lenses", &l); l.Missing != 0 {
		t.Errorf("restore left %d missing", l.Missing)
	}
}
