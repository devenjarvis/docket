package acp_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/devenjarvis/docket/internal/acp"
	"github.com/devenjarvis/docket/internal/acp/acptest"
)

func TestUsageByModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a := &acptest.Agent{Model: "Big", CostPerTurn: 0.25,
		Usage: map[string]any{"totalTokens": 150, "inputTokens": 100, "outputTokens": 50, "cachedReadTokens": 40}}
	c := connect(t, a)
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if got := c.Usage(); len(got) != 0 {
		t.Fatalf("Usage before any turn = %v, want none", got)
	}
	s1, err := c.NewSession(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := c.Prompt(ctx, s1, "hello", nopHandler{}); err != nil {
			t.Fatal(err)
		}
	}
	s2, err := c.NewSession(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetModel(ctx, s2, "m-0"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Prompt(ctx, s2, "hello", nopHandler{}); err != nil {
		t.Fatal(err)
	}
	// The usage_update is a notification sent before the response; give the
	// reader a moment in case it is still being handled.
	time.Sleep(50 * time.Millisecond)
	got := c.Usage()
	if len(got) != 2 {
		t.Fatalf("Usage = %+v, want two models", got)
	}
	big, other := got[0], got[1]
	if big.Model != "Big" || other.Model != "Other" {
		t.Fatalf("models = %q %q", big.Model, other.Model)
	}
	if big.Turns != 2 || big.Input != 200 || big.Output != 100 || big.Total != 300 || big.CacheRead != 80 {
		t.Fatalf("Big tokens = %+v", big)
	}
	// Cost is the session's running total, not a sum of every update.
	if !big.HasCost || math.Abs(big.Cost-0.5) > 1e-9 || big.Currency != "USD" {
		t.Fatalf("Big cost = %v %q (has %v), want 0.5 USD", big.Cost, big.Currency, big.HasCost)
	}
	if other.Turns != 1 || other.Input != 100 || math.Abs(other.Cost-0.25) > 1e-9 {
		t.Fatalf("Other = %+v", other)
	}
}

type nopHandler struct{}

func (nopHandler) Update(acp.SessionUpdate) {}
func (nopHandler) RequestPermission(context.Context, acp.PermissionRequest) (string, bool) {
	return "", false
}
