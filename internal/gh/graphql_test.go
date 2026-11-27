package gh

import (
	"os"
	"testing"
)

func TestParseDetail(t *testing.T) {
	b, err := os.ReadFile("testdata/detail.json")
	if err != nil {
		t.Fatal(err)
	}
	st, checks, conv, err := parseDetail(b)
	if err != nil {
		t.Fatal(err)
	}
	if st.NodeID != "PR_1" || st.Mergeable != "MERGEABLE" || st.MergeStateStatus != "BLOCKED" || st.ReviewDecision != "CHANGES_REQUESTED" {
		t.Errorf("state = %+v", st)
	}
	if st.AutoMerge != "SQUASH" || !st.AutoMergeAllowed || st.HeadRepo != "someone/api" || !st.MaintainerCanModify {
		t.Errorf("state = %+v", st)
	}
	if len(st.Methods) != 2 || st.Methods[0] != "SQUASH" || st.Methods[1] != "REBASE" {
		t.Errorf("methods = %v, want [SQUASH REBASE]", st.Methods)
	}
	if len(st.RequestedReviewers) != 2 || st.RequestedReviewers[1] != "platform" {
		t.Errorf("requested = %v", st.RequestedReviewers)
	}
	if len(checks) != 2 {
		t.Fatalf("checks = %d, want 2", len(checks))
	}
	run, ctx := checks[0], checks[1]
	if !run.Failed() || !run.Required || run.RunID != 500 || run.JobID != 9001 || run.Workflow != "CI" {
		t.Errorf("check run = %+v", run)
	}
	if !ctx.Pending() || ctx.Name != "deploy/preview" || ctx.URL == "" {
		t.Errorf("status context = %+v", ctx)
	}
	if len(conv.Reviews) != 1 || conv.Reviews[0].State != "CHANGES_REQUESTED" {
		t.Errorf("reviews = %+v", conv.Reviews)
	}
	if len(conv.Threads) != 1 || conv.Threads[0].Line != 41 || conv.Threads[0].DiffHunk == "" || len(conv.Threads[0].Comments) != 2 {
		t.Fatalf("threads = %+v", conv.Threads)
	}
	if a := conv.Threads[0].Comments[1].Author; a != "ghost" {
		t.Errorf("a deleted user's comment should read as ghost, got %q", a)
	}
	if len(conv.Comments) != 1 || conv.Comments[0].Author != "bot" {
		t.Errorf("comments = %+v", conv.Comments)
	}
}
