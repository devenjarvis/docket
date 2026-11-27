package review_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/devenjarvis/docket/internal/ledger"
	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/review"
)

func TestReadsAndPostsGoInTheLedger(t *testing.T) {
	var l *ledger.Ledger
	eng, _ := start(t, func(e *review.Engine, home string) {
		l = ledger.Open(home)
		e.Ledger = l
	})
	const key = "acme/payments-api#4127"
	waitReady(t, eng, key)
	for d, opt := range map[string]string{"d1": "block", "d2": "accept", "d3": "accept"} {
		if err := eng.SetAnswer(key, d, model.Answer{Option: opt}); err != nil {
			t.Fatal(err)
		}
	}
	eng.AddTime(key, "d1", 30)
	eng.AddTime(key, "brief", 10)
	draft, err := eng.Compose(key, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Post(context.Background(), key, review.PostInput{Event: draft.Event, Comments: draft.Comments, Receipt: true}); err != nil {
		t.Fatal(err)
	}
	all, err := l.All()
	if err != nil {
		t.Fatal(err)
	}
	var read, post *ledger.Entry
	for i, en := range all {
		if en.Number != 4127 {
			continue
		}
		switch en.Kind {
		case ledger.KindRead:
			read = &all[i]
		case ledger.KindPost:
			post = &all[i]
		}
	}
	if read == nil || read.Failed || read.Tries < 1 || read.Repo != "acme/payments-api" || read.Decisions != 3 {
		t.Fatalf("read entry = %+v", read)
	}
	if post == nil || post.Event != "REQUEST_CHANGES" || post.Answered != 3 || post.Blocking != 1 || !post.Receipt {
		t.Fatalf("post entry = %+v", post)
	}
	if post.YourSeconds != 40 || post.Time["decisions"] != 30 || post.Time["brief"] != 10 {
		t.Fatalf("post time = %d %v", post.YourSeconds, post.Time)
	}
}

func TestARepositoryTurnedOffIsLeftAlone(t *testing.T) {
	eng, _ := start(t, func(e *review.Engine, _ string) {
		e.RepoOff = func(repo string) bool { return repo == "acme/ledger" }
	})
	waitReady(t, eng, "acme/payments-api#4127")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range eng.List().Reviews {
			if strings.HasPrefix(s.Key, "acme/ledger#") {
				t.Fatalf("%s is in the queue though its repository is off", s.Key)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := eng.Add(context.Background(), "acme/ledger#311"); err == nil {
		t.Fatal("added a pull request from a repository that is off")
	}
	for _, m := range eng.Mine() {
		if strings.HasPrefix(m.PR.Key(), "acme/ledger#") {
			t.Fatalf("%s is listed among yours though its repository is off", m.PR.Key())
		}
	}
}
