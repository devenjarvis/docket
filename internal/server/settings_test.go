package server_test

import (
	"strings"
	"testing"

	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/server"
)

func TestRepoSettings(t *testing.T) {
	ts := serve(t)
	if code, body := postJSON(t, ts, "/api/repo", map[string]any{"repo": "not a repo", "off": true}); code != 400 {
		t.Fatalf("a bad name: %d %s", code, body)
	}
	if code, body := postJSON(t, ts, "/api/repo", map[string]any{"repo": "acme/ledger", "tend": "sometimes"}); code != 400 {
		t.Fatalf("a bad tend mode: %d %s", code, body)
	}
	if code, body := postJSON(t, ts, "/api/repo", map[string]any{"repo": "https://github.com/acme/ledger", "off": true, "runCode": "off", "tend": "ask"}); code != 200 {
		t.Fatalf("set: %d %s", code, body)
	}
	c, _ := config.Read()
	if !c.RepoOff("acme/ledger") || c.RunsIn("acme/ledger") || c.TendIn("acme/ledger") != "ask" || !c.RunsIn("acme/web-app") {
		t.Fatalf("config after set: %+v", c.Repos)
	}
	var out struct{ Repos []server.RepoRow }
	getJSON(t, ts, "/api/repos", &out)
	found := false
	for _, r := range out.Repos {
		if r.Repo == "acme/ledger" {
			found = r.Off && r.RunCode == "off" && r.Tend == "ask"
		}
	}
	if !found {
		t.Fatalf("repos = %+v", out.Repos)
	}
	// Back to the defaults leaves nothing behind.
	postJSON(t, ts, "/api/repo", map[string]any{"repo": "acme/ledger", "off": false, "runCode": "default", "tend": "default"})
	if c, _ := config.Read(); len(c.Repos) != 0 {
		t.Fatalf("defaults should clear the entry: %+v", c.Repos)
	}
}

func TestSettingsValidatesNewFields(t *testing.T) {
	ts := serve(t)
	if code, _ := postJSON(t, ts, "/api/settings", map[string]any{"concurrency": 9}); code != 400 {
		t.Fatalf("concurrency 9 accepted")
	}
	if code, _ := postJSON(t, ts, "/api/settings", map[string]any{"prices": map[string]any{"m": map[string]float64{"input": -1}}}); code != 400 {
		t.Fatalf("a negative price accepted")
	}
	if code, body := postJSON(t, ts, "/api/settings", map[string]any{"concurrency": 3, "notifyOff": []string{"activity"},
		"agentPath": []string{" /opt/bin ", ""}, "prices": map[string]any{"Big": map[string]float64{"input": 3, "output": 15}}}); code != 200 {
		t.Fatalf("save: %d %s", code, body)
	}
	c, _ := config.Read()
	if c.Workers() != 3 || c.Notifies("activity") || !c.Notifies("read") || len(c.AgentPath) != 1 || c.AgentPath[0] != "/opt/bin" || c.Prices["Big"].Output != 15 {
		t.Fatalf("config = %+v", c)
	}
}

func TestMetricsAndData(t *testing.T) {
	ts := serve(t)
	var m struct {
		Report struct {
			Signed int
			Empty  bool
		}
	}
	if code := getJSON(t, ts, "/api/metrics?days=30", &m); code != 200 || !m.Report.Empty {
		t.Fatalf("metrics on an empty ledger: %d %+v", code, m)
	}
	var d struct {
		Parts []struct{ Path string }
	}
	if code := getJSON(t, ts, "/api/data", &d); code != 200 || len(d.Parts) == 0 {
		t.Fatalf("data: %d %+v", code, d)
	}
	if code, body := postJSON(t, ts, "/api/ledger/clear", nil); code != 200 || !strings.Contains(body, "ok") {
		t.Fatalf("clear: %d %s", code, body)
	}
}
