package review_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devenjarvis/docket/internal/demo"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/review"
)

func startSelf(t *testing.T, mode string) (*review.Engine, string) {
	t.Helper()
	return start(t, func(e *review.Engine, home string) {
		e.Branches = demo.Checkout{Home: home}
		e.TendMode = func(string) string { return "auto" }
		e.SelfMode = func(string) string { return mode }
	})
}

func row(t *testing.T, eng *review.Engine, key string) review.Item {
	t.Helper()
	for _, it := range eng.Unified(time.Now()) {
		if it.Key == key {
			return it
		}
	}
	t.Fatalf("%s is not in the inbox", key)
	return review.Item{}
}

// waitFor polls until ok holds.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// The whole loop on a draft nobody has reviewed: the agent reviews it for
// its author, the author settles the findings, the agent fixes what they
// chose to fix through the same gate as any step, and the read after the
// push says the findings are resolved.
func TestASelfReviewFindsFixesAndRereads(t *testing.T) {
	eng, _ := startSelf(t, "auto")
	ctx := context.Background()
	if err := eng.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	v := waitReady(t, eng, demo.DraftKey)
	if !v.Self || len(v.Brief.Decisions) != 2 || v.Brief.Summary == "" {
		t.Fatalf("self-review: self=%v decisions=%d summary=%q", v.Self, len(v.Brief.Decisions), v.Brief.Summary)
	}
	// A self-review is never a review row of its own.
	for _, s := range eng.List().Reviews {
		if s.Key == demo.DraftKey || s.PR.Author == "devenjarvis" {
			t.Fatalf("a self-review shows as a review: %+v", s)
		}
	}
	it := row(t, eng, demo.DraftKey)
	if !it.Mine || it.Band != review.BandCloser || it.Action != "Self-review" || it.Decisions != 2 {
		t.Fatalf("the draft's row should ask for its self-review: %+v", it)
	}

	// Settle: fix both.
	for _, d := range []string{"d1", "d2"} {
		if err := eng.SetAnswer(demo.DraftKey, d, model.Answer{Option: "a"}); err != nil {
			t.Fatal(err)
		}
	}
	if it := row(t, eng, demo.DraftKey); it.Band != review.BandQuick || it.Action != "Wrap up" || !strings.Contains(it.Why, "hand two fixes") {
		t.Fatalf("a settled self-review with fixes should be wrapped up: %+v", it)
	}
	// Nothing posts for a fix, and a verdict is refused.
	d, err := eng.Compose(demo.DraftKey, true)
	if err != nil || d.Event != "COMMENT" || len(d.Comments) != 0 || len(d.Locks) != 0 || d.ReceiptMarkdown != "" {
		t.Fatalf("draft = %+v, %v", d, err)
	}
	if _, err := eng.Post(ctx, demo.DraftKey, review.PostInput{Event: "APPROVE"}); err == nil {
		t.Fatal("approving your own pull request should be refused")
	}

	// Hand the fixes to the agent.
	if err := eng.HandOff(demo.DraftKey, "agent"); err != nil {
		t.Fatal(err)
	}
	m := waitTend(t, eng, demo.DraftKey)
	if len(m.Tend.Tasks) != 2 || !strings.Contains(m.Tend.Trigger, "self-review") {
		t.Fatalf("the run should carry the findings: %+v", m.Tend)
	}
	fix := action(t, m, "a1")
	if strings.Join(fix.Tasks, ",") != "d1,d2" || fix.Verdict == nil || fix.Verdict.Decision != model.GateAsk || fix.State != model.StateWaiting {
		t.Fatalf("the fix should name both findings and wait, since it changes what goes to the gateway: %+v", fix)
	}
	// The fix waits for the author, so the row says so.
	if it := row(t, eng, demo.DraftKey); it.Action != "Check fix" {
		t.Fatalf("a held fix comes first: %+v", it)
	}
	if err := eng.Decide(ctx, demo.DraftKey, "a1", "approve", "", ""); err != nil {
		t.Fatal(err)
	}

	// The push is a new head, so the self-review reads it again.
	waitFor(t, "a read of the fixed head", func() bool {
		v, ok := eng.Get(demo.DraftKey)
		return ok && v.Status == "ready" && v.ReadSHA == "demo-fix-refund-idempotency-and-log"
	})
	v, _ = eng.Get(demo.DraftKey)
	if len(v.Brief.Decisions) != 0 || v.Brief.Since == nil || strings.Join(v.Brief.Since.Resolved, ",") != "d1,d2" {
		t.Fatalf("the re-read should resolve both findings: %+v", v.Brief.Since)
	}
	if it := row(t, eng, demo.DraftKey); it.Action != "Mark ready" || strings.Contains(it.Why, "self-review") {
		t.Fatalf("with the findings fixed, the draft's next move is to mark it ready: %+v", it)
	}
}

// A note for reviewers posts as a comment on the author's own pull request,
// in their voice, with no label and no receipt.
func TestASelfReviewNotePostsAsAComment(t *testing.T) {
	eng, home := startSelf(t, "auto")
	ctx := context.Background()
	if err := eng.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	waitReady(t, eng, demo.DraftKey)
	_ = eng.SetAnswer(demo.DraftKey, "d1", model.Answer{Option: "b"})
	_ = eng.SetAnswer(demo.DraftKey, "d2", model.Answer{Option: "c"})
	d, err := eng.Compose(demo.DraftKey, true)
	if err != nil || len(d.Comments) != 1 || d.Comments[0].Decision != "d1" || !d.Comments[0].Inline {
		t.Fatalf("draft = %+v, %v", d, err)
	}
	if it := row(t, eng, demo.DraftKey); it.Action != "Wrap up" || !strings.Contains(it.Why, "post one note") {
		t.Fatalf("an unposted note should be wrapped up: %+v", it)
	}
	if _, err := eng.Post(ctx, demo.DraftKey, review.PostInput{Event: "COMMENT", Comments: d.Comments, Receipt: true}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, "posted", "payments-api-4140.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); !strings.Contains(s, "gateway dedupes refunds by id") || strings.Contains(s, "**Note") || strings.Contains(s, "How this was reviewed") {
		t.Fatalf("posted = %s", s)
	}
	if it := row(t, eng, demo.DraftKey); it.Action == "Wrap up" {
		t.Fatalf("once posted, the note is done: %+v", it)
	}
}

// In ask mode nothing is read until the author asks; off removes it.
func TestSelfReviewModes(t *testing.T) {
	mode := "ask"
	eng, _ := start(t, func(e *review.Engine, home string) {
		e.SelfMode = func(string) string { return mode }
	})
	ctx := context.Background()
	if err := eng.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := eng.Get(demo.DraftKey); ok {
		t.Fatal("ask mode should not start a self-review on its own")
	}
	if err := eng.SelfReview(demo.DraftKey); err != nil {
		t.Fatal(err)
	}
	waitReady(t, eng, demo.DraftKey)
	mode = "off"
	if err := eng.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := eng.Get(demo.DraftKey); ok {
		t.Fatal("turning self-review off should remove it")
	}
	if err := eng.SelfReview(demo.DraftKey); err == nil || !strings.Contains(err.Error(), "off") {
		t.Fatalf("asking while it is off: %v", err)
	}
	// Adding one of your own by hand is a self-review, not a review.
	mode = "auto"
	key, err := eng.Add(ctx, "acme/payments-api#4140")
	if err != nil || key != demo.DraftKey {
		t.Fatalf("add = %q, %v", key, err)
	}
	if v := waitReady(t, eng, demo.DraftKey); !v.Self {
		t.Fatal("an own pull request added by hand should be a self-review")
	}
}
