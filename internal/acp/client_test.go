package acp_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/devenjarvis/docket/internal/acp"
	"github.com/devenjarvis/docket/internal/acp/acptest"
)

func TestSmallest(t *testing.T) {
	tests := []struct {
		name   string
		models []acp.ModelInfo
		want   string
		ok     bool
	}{
		{
			name: "haiku beats mini",
			models: []acp.ModelInfo{
				{ID: "gpt-mini", Name: "GPT Mini"},
				{ID: "opus", Name: "Opus"},
				{ID: "claude-3", Name: "Claude Haiku"},
			},
			want: "claude-3", ok: true,
		},
		{
			name: "mini found by name only",
			models: []acp.ModelInfo{
				{ID: "big", Name: "Big"},
				{ID: "m-2", Name: "Model MINI"},
			},
			want: "m-2", ok: true,
		},
		{
			name: "mini preferred over flash",
			models: []acp.ModelInfo{
				{ID: "fast-2", Name: "Flash"},
				{ID: "o4-mini", Name: "o4"},
			},
			want: "o4-mini", ok: true,
		},
		{
			name:   "nothing matches",
			models: []acp.ModelInfo{{ID: "opus", Name: "Opus"}, {ID: "sonnet", Name: "Sonnet"}},
			want:   "", ok: false,
		},
		{
			name:   "gemini is not mini",
			models: []acp.ModelInfo{{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro"}, {ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash"}},
			want:   "gemini-2.5-flash", ok: true,
		},
		{name: "empty list", models: nil, want: "", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := acp.Smallest(tt.models)
			if got != tt.want || ok != tt.ok {
				t.Errorf("Smallest = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func connect(t *testing.T, a *acptest.Agent) *acp.Client {
	t.Helper()
	cr, aw := io.Pipe()
	ar, cw := io.Pipe()
	go func() { _ = a.Serve(ar, aw); _ = aw.Close() }()
	c := acp.NewClient(cr, cw, func() error { return cw.Close() })
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestModelsAndSetModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a := &acptest.Agent{Model: "Big"}
	c := connect(t, a)
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if got := c.Models(); got != nil {
		t.Fatalf("Models before session/new = %v, want nil", got)
	}
	sid, err := c.NewSession(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	ms := c.Models()
	want := []acp.ModelInfo{{ID: "m-0", Name: "Other"}, {ID: "m-1", Name: "Big"}}
	if len(ms) != len(want) || ms[0] != want[0] || ms[1] != want[1] {
		t.Fatalf("Models = %v, want %v", ms, want)
	}
	ms[0].Name = "mutated"
	if c.Models()[0].Name != "Other" {
		t.Fatal("Models did not return a copy")
	}
	if c.Model() != "Big" {
		t.Fatalf("Model = %q, want Big", c.Model())
	}

	if err := c.SetModel(ctx, sid, "m-0"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	got := a.ModelChoices()
	if len(got) != 1 || got[0] != (acptest.ModelChoice{SessionID: sid, ModelID: "m-0"}) {
		t.Fatalf("agent saw set_model %v, want [{%s m-0}]", got, sid)
	}
	if c.Model() != "Other" {
		t.Fatalf("Model after SetModel = %q, want Other", c.Model())
	}

	if err := c.SetModel(ctx, sid, "unlisted"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if c.Model() != "unlisted" {
		t.Fatalf("Model after SetModel to unknown id = %q, want the id", c.Model())
	}
}

func TestSetModelAgentError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a := &acptest.Agent{Model: "Big", RejectSetModel: true}
	c := connect(t, a)
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	sid, err := c.NewSession(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetModel(ctx, sid, "m-0"); err == nil {
		t.Fatal("SetModel succeeded, want the agent's error")
	}
	if c.Model() != "Big" {
		t.Fatalf("Model after failed SetModel = %q, want Big", c.Model())
	}
}

func TestModelsNilWhenNotReported(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := connect(t, &acptest.Agent{})
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.NewSession(ctx, "/tmp"); err != nil {
		t.Fatal(err)
	}
	if got := c.Models(); got != nil {
		t.Fatalf("Models = %v, want nil", got)
	}
}
