// Package server is Docket's HTTP face: the JSON API the UI calls and the
// static files it is made of. The desktop app serves it straight into its
// window; `docket serve` serves it on a local port for a browser.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/agents"
	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/review"
	"github.com/devenjarvis/docket/internal/rules"
	"github.com/devenjarvis/docket/internal/web"
)

type Server struct {
	Eng   *review.Engine
	Rules *rules.Store
	Home  string
	// Demo is set when the sample pull requests and scripted agent are in
	// use; the UI says so.
	Demo bool
	// DemoAgent, in demo mode, is the command for the scripted agent.
	DemoAgent string
	// OpenURL opens a link in the user's browser. The desktop app sets it.
	OpenURL func(url string)

	mux *http.ServeMux
}

func New(eng *review.Engine, rs *rules.Store, home string) *Server {
	s := &Server{Eng: eng, Rules: rs, Home: home, mux: http.NewServeMux()}
	static, _ := fs.Sub(web.FS(), ".")
	fileServer := http.FileServer(http.FS(static))
	s.mux.Handle("/static/", http.StripPrefix("/static/", fileServer))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, err := fs.ReadFile(static, "index.html")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})
	s.mux.HandleFunc("GET /api/state", s.state)
	s.mux.HandleFunc("GET /api/review", s.review)
	s.mux.HandleFunc("GET /api/draft", s.draft)
	s.mux.HandleFunc("GET /api/events", s.events)
	s.mux.HandleFunc("GET /api/rules", s.rules)
	s.mux.HandleFunc("GET /api/out", s.out)
	s.mux.HandleFunc("POST /api/sync", s.sync)
	s.mux.HandleFunc("POST /api/add", s.add)
	s.mux.HandleFunc("POST /api/reread", s.reread)
	s.mux.HandleFunc("POST /api/remove", s.remove)
	s.mux.HandleFunc("POST /api/answer", s.answer)
	s.mux.HandleFunc("POST /api/time", s.time)
	s.mux.HandleFunc("POST /api/audit", s.audit)
	s.mux.HandleFunc("POST /api/notes", s.notes)
	s.mux.HandleFunc("POST /api/ask", s.ask)
	s.mux.HandleFunc("POST /api/post", s.post)
	s.mux.HandleFunc("POST /api/rule", s.saveRule)
	s.mux.HandleFunc("POST /api/settings", s.settings)
	s.mux.HandleFunc("POST /api/open", s.open)
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

// AgentCommand resolves the agent to run: the scripted one in demo mode, the
// configured one, else the first that is ready on this machine.
func (s *Server) AgentCommand() (command, name string) {
	if s.Demo {
		return s.DemoAgent, "Scripted demo agent"
	}
	c, _ := config.Read()
	if c.AgentCommand != "" {
		return c.AgentCommand, agents.DisplayName(c.AgentCommand)
	}
	if a, ok := agents.ByID(c.Agent); ok {
		return a.Command, a.Name
	}
	for _, d := range agents.Detect(exec.LookPath, "") {
		if d.Ready {
			return d.Command, d.Name
		}
	}
	return "", ""
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func read[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&v); err != nil {
		fail(w, 400, fmt.Errorf("bad request: %w", err))
		return v, false
	}
	return v, true
}

func (s *Server) state(w http.ResponseWriter, _ *http.Request) {
	c, _ := config.Read()
	cmd, name := s.AgentCommand()
	all, _ := s.Rules.List()
	active := 0
	for _, r := range all {
		if r.Status != "retired" {
			active++
		}
	}
	writeJSON(w, map[string]any{
		"queue":  s.Eng.List(),
		"agent":  map[string]any{"command": cmd, "name": name},
		"agents": agents.Detect(exec.LookPath, ""),
		"config": map[string]any{
			"agent": c.Agent, "agentCommand": c.AgentCommand, "pollSeconds": c.Poll(),
			"theme": c.Theme, "receipt": c.Receipt(), "runCode": s.Eng.RunCode(),
		},
		"rules": active,
		"demo":  s.Demo,
		"home":  s.Home,
	})
}

func (s *Server) review(w http.ResponseWriter, r *http.Request) {
	v, ok := s.Eng.Get(r.URL.Query().Get("key"))
	if !ok {
		fail(w, 404, fmt.Errorf("no such review"))
		return
	}
	writeJSON(w, v)
}

func (s *Server) draft(w http.ResponseWriter, r *http.Request) {
	c, _ := config.Read()
	d, err := s.Eng.Compose(r.URL.Query().Get("key"), c.Receipt())
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, d)
}

// events is a server-sent event stream of everything that changes.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	ch, stop := s.Eng.Hub.Subscribe()
	defer stop()
	fmt.Fprint(w, ": hello\n\n")
	fl.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		case e := <-ch:
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		fl.Flush()
	}
}

func (s *Server) sync(w http.ResponseWriter, r *http.Request) {
	if err := s.Eng.Sync(r.Context()); err != nil {
		fail(w, 502, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) add(w http.ResponseWriter, r *http.Request) {
	in, ok := read[struct{ Ref string }](w, r)
	if !ok {
		return
	}
	key, err := s.Eng.Add(r.Context(), in.Ref)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]string{"key": key})
}

type keyed struct {
	Key string `json:"key"`
}

func (s *Server) reread(w http.ResponseWriter, r *http.Request) {
	in, ok := read[keyed](w, r)
	if !ok {
		return
	}
	if err := s.Eng.Reread(in.Key); err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	in, ok := read[keyed](w, r)
	if !ok {
		return
	}
	s.Eng.Remove(in.Key)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) answer(w http.ResponseWriter, r *http.Request) {
	in, ok := read[struct {
		Key      string `json:"key"`
		Decision string `json:"decision"`
		model.Answer
	}](w, r)
	if !ok {
		return
	}
	if err := s.Eng.SetAnswer(in.Key, in.Decision, in.Answer); err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) time(w http.ResponseWriter, r *http.Request) {
	in, ok := read[struct {
		Key     string `json:"key"`
		ID      string `json:"id"`
		Seconds int    `json:"seconds"`
	}](w, r)
	if !ok {
		return
	}
	s.Eng.AddTime(in.Key, in.ID, in.Seconds)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	in, ok := read[struct {
		Key     string `json:"key"`
		Section string `json:"section"`
		Done    bool   `json:"done"`
	}](w, r)
	if !ok {
		return
	}
	s.Eng.Audit(in.Key, in.Section, in.Done)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) notes(w http.ResponseWriter, r *http.Request) {
	in, ok := read[struct {
		Key   string       `json:"key"`
		Notes []model.Note `json:"notes"`
	}](w, r)
	if !ok {
		return
	}
	s.Eng.SetNotes(in.Key, in.Notes)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) ask(w http.ResponseWriter, r *http.Request) {
	in, ok := read[struct {
		Key      string `json:"key"`
		Decision string `json:"decision"`
		Question string `json:"question"`
	}](w, r)
	if !ok {
		return
	}
	id, err := s.Eng.Ask(r.Context(), in.Key, in.Decision, in.Question)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]string{"id": id})
}

func (s *Server) post(w http.ResponseWriter, r *http.Request) {
	in, ok := read[struct {
		Key string `json:"key"`
		review.PostInput
	}](w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	url, err := s.Eng.Post(ctx, in.Key, in.PostInput)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]string{"url": url})
}

func (s *Server) rules(w http.ResponseWriter, _ *http.Request) {
	all, err := s.Rules.List()
	if err != nil {
		fail(w, 500, err)
		return
	}
	type row struct {
		model.Rule
		Fading bool `json:"fading"`
	}
	out := make([]row, 0, len(all))
	for _, r := range all {
		out = append(out, row{r, r.Fading()})
	}
	writeJSON(w, map[string]any{"rules": out, "history": s.Rules.History(12), "dir": s.Rules.Dir()})
}

func (s *Server) saveRule(w http.ResponseWriter, r *http.Request) {
	in, ok := read[struct {
		model.Rule
		Why string `json:"why"`
	}](w, r)
	if !ok {
		return
	}
	if in.ID != "" {
		// Keep what the form does not send.
		if cur, ok := s.Rules.Get(in.ID); ok {
			if in.Origin == "" {
				in.Rule.Origin = cur.Origin
			}
			if in.Created.IsZero() {
				in.Rule.Created = cur.Created
			}
		}
	}
	if in.Rule.Origin == "" {
		in.Rule.Origin = "Written by you"
	}
	saved, err := s.Rules.Save(in.Rule, in.Why)
	if err != nil {
		fail(w, 400, err)
		return
	}
	s.Eng.Hub.Send(review.Event{Type: "rules"})
	writeJSON(w, saved)
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	in, ok := read[struct {
		Agent        *string `json:"agent"`
		AgentCommand *string `json:"agentCommand"`
		PollSeconds  *int    `json:"pollSeconds"`
		Theme        *string `json:"theme"`
		Receipt      *bool   `json:"receipt"`
		RunCode      *bool   `json:"runCode"`
	}](w, r)
	if !ok {
		return
	}
	c, _ := config.Read()
	if in.Agent != nil {
		c.Agent = *in.Agent
		c.AgentCommand = ""
	}
	if in.AgentCommand != nil {
		c.AgentCommand = strings.TrimSpace(*in.AgentCommand)
	}
	if in.PollSeconds != nil {
		c.PollSeconds = *in.PollSeconds
	}
	if in.Theme != nil {
		c.Theme = *in.Theme
	}
	if in.Receipt != nil {
		c.ReceiptInReview = in.Receipt
	}
	if in.RunCode != nil {
		c.RunCode = in.RunCode
	}
	if err := config.Write(c); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// out serves a file the agent saved for a review (a screenshot, a probe).
func (s *Server) out(w http.ResponseWriter, r *http.Request) {
	v, ok := s.Eng.Get(r.URL.Query().Get("key"))
	name := filepath.Base(r.URL.Query().Get("file"))
	if !ok || name == "." || name == "/" {
		http.NotFound(w, r)
		return
	}
	p := filepath.Join(v.OutDir, name)
	if _, err := os.Stat(p); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, p)
}

func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	in, ok := read[struct{ URL string }](w, r)
	if !ok {
		return
	}
	if !strings.HasPrefix(in.URL, "https://") {
		fail(w, 400, fmt.Errorf("only https links can be opened"))
		return
	}
	if s.OpenURL != nil {
		s.OpenURL(in.URL)
	}
	writeJSON(w, map[string]bool{"native": s.OpenURL != nil})
}
