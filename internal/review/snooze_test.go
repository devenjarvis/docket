package review_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devenjarvis/docket/internal/demo"
	"github.com/devenjarvis/docket/internal/review"
)

func findItem(items []review.Item, key string) (review.Item, bool) {
	for _, it := range items {
		if it.Key == key {
			return it, true
		}
	}
	return review.Item{}, false
}

func TestSnoozeKeepsAndWakes(t *testing.T) {
	eng, home := start(t)
	var key string
	for i := 0; i < 200 && key == ""; i++ {
		if rs := eng.List().Reviews; len(rs) > 0 {
			key = rs[0].Key
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if key == "" {
		t.Fatal("no review to snooze")
	}
	now := time.Now()
	if err := eng.SnoozeKey(key, review.SnoozeTomorrow, now); err != nil {
		t.Fatal(err)
	}
	if it, _ := findItem(eng.Unified(now), key); it.Snoozed != review.SnoozeTomorrow || it.Band != review.BandWaiting {
		t.Fatalf("after snoozing: %+v", it)
	}
	if _, err := os.Stat(filepath.Join(home, "snoozed.json")); err != nil {
		t.Fatalf("the snooze was not saved: %v", err)
	}

	// A fresh engine on the same home still has it snoozed.
	again := review.New(home, demo.GitHub{Home: home}, demo.Checkout{Home: home}, eng.Rules)
	if it, _ := findItem(again.Unified(now), key); it.Snoozed == "" {
		t.Fatalf("the snooze did not survive a restart: %+v", it)
	}

	// Waking it by hand brings it back.
	if err := eng.SnoozeKey(key, "", now); err != nil {
		t.Fatal(err)
	}
	if it, _ := findItem(eng.Unified(now), key); it.Snoozed != "" {
		t.Fatalf("after waking: %+v", it)
	}

	// A tomorrow snooze ends on its own, and is dropped.
	if err := eng.SnoozeKey(key, review.SnoozeTomorrow, now); err != nil {
		t.Fatal(err)
	}
	if it, _ := findItem(eng.Unified(now.Add(48*time.Hour)), key); it.Snoozed != "" {
		t.Fatalf("two days later: %+v", it)
	}

	if err := eng.SnoozeKey("o/nope#1", review.SnoozeChange, now); err == nil {
		t.Error("snoozed a pull request that is not in the inbox")
	}
	if err := eng.SnoozeKey(key, "forever", now); err == nil {
		t.Error("took an unknown snooze kind")
	}
}
