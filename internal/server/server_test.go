package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/devenjarvis/docket/internal/demo"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/review"
	"github.com/devenjarvis/docket/internal/rules"
	"github.com/devenjarvis/docket/internal/server"
)

func serve(t *testing.T) *httptest.Server {
	t.Helper()
	home := t.TempDir()
	t.Setenv("DOCKET_HOME", home)
	rs, err := rules.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	g := demo.GitHub{Home: home}
	eng := review.New(home, g, demo.Checkout{Home: home}, rs)
	eng.Inbox = g
	if err := eng.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.New(eng, rs, home).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func postJSON(t *testing.T, ts *httptest.Server, path string, body any) (int, string) {
	t.Helper()
	b, _ := json.Marshal(body)
	res, err := http.Post(ts.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(res.Body)
	return res.StatusCode, buf.String()
}

func getJSON(t *testing.T, ts *httptest.Server, path string, out any) int {
	t.Helper()
	res, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func TestActRoutes(t *testing.T) {
	ts := serve(t)
	var mine struct {
		Mine []model.Mine `json:"mine"`
	}
	if getJSON(t, ts, "/api/mine", &mine) != 200 || len(mine.Mine) != 3 {
		t.Fatalf("GET /api/mine = %+v", mine)
	}
	if code, body := postJSON(t, ts, "/api/act", map[string]any{"key": demo.BlockedKey, "kind": "explode"}); code != 400 {
		t.Errorf("unknown kind: %d %s", code, body)
	}
	if code, body := postJSON(t, ts, "/api/act", map[string]any{"key": demo.ReadyKey, "kind": "merge", "method": "SQUASH"}); code != 400 || !strings.Contains(body, "confirm") {
		t.Errorf("merge without confirm: %d %s", code, body)
	}

	var m model.Mine
	getJSON(t, ts, "/api/mine/pr?key="+url.QueryEscape(demo.BlockedKey), &m)
	if len(m.Conversation.Threads) == 0 {
		t.Fatalf("no threads on %s", demo.BlockedKey)
	}
	if code, body := postJSON(t, ts, "/api/act", map[string]any{"key": demo.BlockedKey, "kind": "reply", "thread": m.Conversation.Threads[0].ID, "body": "Renamed."}); code != 200 {
		t.Fatalf("reply: %d %s", code, body)
	}
	var conv model.Conversation
	getJSON(t, ts, "/api/conversation?key="+url.QueryEscape(demo.BlockedKey), &conv)
	if conv.Threads[0].Last().Body != "Renamed." {
		t.Errorf("the reply is not in the conversation: %+v", conv.Threads[0])
	}

	var log struct {
		Log string `json:"log"`
	}
	job := ""
	for _, c := range m.Checks {
		if c.Failed() {
			job = strconv.FormatInt(c.JobID, 10)
		}
	}
	if getJSON(t, ts, "/api/log?key="+url.QueryEscape(demo.BlockedKey)+"&job="+job, &log) != 200 || !strings.Contains(log.Log, "nil map") {
		t.Errorf("failed log = %q", log.Log)
	}

	if code, body := postJSON(t, ts, "/api/act", map[string]any{"key": demo.ReadyKey, "kind": "merge", "method": "SQUASH", "confirm": true}); code != 200 {
		t.Errorf("confirmed merge: %d %s", code, body)
	}
}

func TestTendRoutes(t *testing.T) {
	ts := serve(t)
	if code, body := postJSON(t, ts, "/api/decide", map[string]any{"key": demo.BlockedKey, "action": "a1", "choice": "explode"}); code != 400 {
		t.Errorf("unknown choice: %d %s", code, body)
	}
	if code, body := postJSON(t, ts, "/api/settings", map[string]any{"tend": "sometimes"}); code != 400 {
		t.Errorf("a bad tend mode was saved: %d %s", code, body)
	}
	if code, body := postJSON(t, ts, "/api/settings", map[string]any{"tend": "ask"}); code != 200 {
		t.Fatalf("settings: %d %s", code, body)
	}
	var st struct {
		Config struct {
			Tend string `json:"tend"`
		} `json:"config"`
	}
	getJSON(t, ts, "/api/state", &st)
	if st.Config.Tend != "ask" {
		t.Errorf("tend mode = %q, want ask", st.Config.Tend)
	}
}

func TestUndoOfAnUnknownStepFails(t *testing.T) {
	ts := serve(t)
	if code, body := postJSON(t, ts, "/api/undo", map[string]any{"key": demo.BlockedKey, "action": "a9"}); code != 400 {
		t.Errorf("undo of an unknown step: %d %s", code, body)
	}
}
