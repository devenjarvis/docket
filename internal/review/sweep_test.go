package review_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncRemovesCheckoutsNoReviewNeeds(t *testing.T) {
	eng, home := start(t)
	const key = "acme/payments-api#4127"
	v := waitReady(t, eng, key)
	current := filepath.Dir(v.OutDir)

	reviews := filepath.Join(home, "reviews")
	closed := filepath.Join(reviews, "acme", "gone", "7", "abcdef1234")
	older := filepath.Join(reviews, "acme", "payments-api", "4127", "0ld0ld0ld0")
	for _, d := range []string{closed, older} {
		if err := os.MkdirAll(filepath.Join(d, "head"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// A read keeps its heads until it has fully finished, so the sweep may
	// take a second sync to reach the older head.
	gone := func(d string) bool { _, err := os.Stat(d); return os.IsNotExist(err) }
	deadline := time.Now().Add(5 * time.Second)
	for !(gone(older) && gone(filepath.Join(reviews, "acme", "gone"))) {
		if time.Now().After(deadline) {
			t.Fatalf("old checkouts are still there: older=%v closed=%v", !gone(older), !gone(closed))
		}
		if err := eng.Sync(context.Background()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(current, "out")); err != nil {
		t.Errorf("the checkout the brief was read at must stay: %v", err)
	}
}
