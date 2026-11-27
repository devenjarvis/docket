package model

import (
	"fmt"
	"time"
)

// Mine is one open pull request the user wrote, with everything that decides
// whether it can merge.
type Mine struct {
	PR           PR           `json:"pr"`
	State        MergeState   `json:"state"`
	Checks       []Check      `json:"checks"`
	Conversation Conversation `json:"conversation"`
	Blockers     []Blocker    `json:"blockers"`
	// Lane is needs-you, waiting or ready.
	Lane      string    `json:"lane"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// MergeState is what GitHub says about whether a pull request can merge.
type MergeState struct {
	// NodeID is the pull request's GraphQL id, which mutations need.
	NodeID string `json:"nodeId"`
	// Mergeable is MERGEABLE, CONFLICTING or UNKNOWN (not computed yet).
	Mergeable string `json:"mergeable"`
	// MergeStateStatus is CLEAN, BEHIND, BLOCKED, DIRTY, DRAFT, HAS_HOOKS,
	// UNSTABLE or UNKNOWN.
	MergeStateStatus string `json:"mergeStateStatus"`
	// ReviewDecision is APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or
	// empty when the repository requires no review.
	ReviewDecision string `json:"reviewDecision"`
	// AutoMerge is the method auto-merge is set to, or empty when it is off.
	AutoMerge string `json:"autoMerge,omitempty"`
	// Methods are the merge methods the repository allows: MERGE, SQUASH,
	// REBASE.
	Methods []string `json:"methods"`
	// AutoMergeAllowed says whether the repository allows auto-merge.
	AutoMergeAllowed bool `json:"autoMergeAllowed"`
	// HeadRepo is owner/name of the repository the branch lives in, which
	// differs from the PR's repository for a fork.
	HeadRepo            string   `json:"headRepo"`
	MaintainerCanModify bool     `json:"maintainerCanModify"`
	RequestedReviewers  []string `json:"requestedReviewers"`
}

// Check is one CI check run or commit status, in one shape.
type Check struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Workflow string `json:"workflow,omitempty"`
	// Status is QUEUED, IN_PROGRESS, PENDING or COMPLETED.
	Status string `json:"status"`
	// Conclusion, once COMPLETED, is SUCCESS, FAILURE, NEUTRAL, SKIPPED,
	// CANCELLED, TIMED_OUT, ACTION_REQUIRED, STARTUP_FAILURE or STALE.
	Conclusion  string    `json:"conclusion,omitempty"`
	Required    bool      `json:"required"`
	URL         string    `json:"url,omitempty"`
	StartedAt   time.Time `json:"startedAt,omitempty"`
	CompletedAt time.Time `json:"completedAt,omitempty"`
	// RunID and JobID are set for GitHub Actions jobs, so the failed log can
	// be read and the run re-run.
	RunID int64 `json:"runId,omitempty"`
	JobID int64 `json:"jobId,omitempty"`
}

// Failed reports whether the check finished and did not pass.
func (c Check) Failed() bool {
	if c.Status != "COMPLETED" {
		return false
	}
	switch c.Conclusion {
	case "FAILURE", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE", "ERROR":
		return true
	}
	return false
}

// Pending reports whether the check has not finished.
func (c Check) Pending() bool { return c.Status != "COMPLETED" }

// Conversation is everything people have said on a pull request.
type Conversation struct {
	Reviews  []ReviewEvent `json:"reviews"`
	Threads  []Thread      `json:"threads"`
	Comments []Comment     `json:"comments"`
}

// ReviewEvent is one submitted review.
type ReviewEvent struct {
	ID     string `json:"id"`
	Author string `json:"author"`
	// State is APPROVED, CHANGES_REQUESTED, COMMENTED or DISMISSED.
	State string    `json:"state"`
	Body  string    `json:"body"`
	URL   string    `json:"url,omitempty"`
	At    time.Time `json:"at"`
}

// Thread is one line-anchored review conversation.
type Thread struct {
	ID       string    `json:"id"`
	Path     string    `json:"path"`
	Line     int       `json:"line,omitempty"`
	Resolved bool      `json:"resolved"`
	Outdated bool      `json:"outdated"`
	DiffHunk string    `json:"diffHunk,omitempty"`
	Comments []Comment `json:"comments"`
}

// Last is the thread's latest comment, or a zero Comment.
func (t Thread) Last() Comment {
	if len(t.Comments) == 0 {
		return Comment{}
	}
	return t.Comments[len(t.Comments)-1]
}

type Comment struct {
	ID     string    `json:"id"`
	Author string    `json:"author"`
	Body   string    `json:"body"`
	URL    string    `json:"url,omitempty"`
	At     time.Time `json:"at"`
}

// Blocker kinds: the closed set of reasons a pull request cannot merge yet.
const (
	BlockDraft            = "draft"
	BlockConflicts        = "conflicts"
	BlockBehind           = "behind"
	BlockChecksFailing    = "checks-failing"
	BlockChecksPending    = "checks-pending"
	BlockChangesRequested = "changes-requested"
	BlockReviewRequired   = "review-required"
	BlockThreads          = "unresolved-threads"
)

// Lanes for the user's own pull requests.
const (
	LaneNeedsYou = "needs-you"
	LaneWaiting  = "waiting"
	LaneReady    = "ready"
)

// Blocker is one reason a pull request cannot merge yet.
type Blocker struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	// OnYou is true when the next move is the author's.
	OnYou bool `json:"onYou"`
}

// Blockers lists, in a fixed order, what stands between a pull request and
// merging. An unknown mergeability is not a blocker: GitHub computes it
// lazily, and the next look will know.
func Blockers(viewer string, draft bool, m MergeState, checks []Check, conv Conversation) []Blocker {
	var out []Blocker
	add := func(kind, text string, onYou bool) { out = append(out, Blocker{Kind: kind, Text: text, OnYou: onYou}) }
	if draft {
		add(BlockDraft, "It is a draft: mark it ready for review.", true)
	}
	if m.Mergeable == "CONFLICTING" || m.MergeStateStatus == "DIRTY" {
		add(BlockConflicts, "It conflicts with the base branch.", true)
	} else if m.MergeStateStatus == "BEHIND" {
		add(BlockBehind, "The branch is behind its base and the repository requires it to be up to date.", true)
	}
	failing, pending := 0, 0
	for _, c := range checks {
		switch {
		case c.Failed():
			failing++
		case c.Pending():
			pending++
		}
	}
	if failing > 0 {
		add(BlockChecksFailing, fmt.Sprintf("%d check%s failing.", failing, plural(failing)), true)
	}
	if pending > 0 {
		add(BlockChecksPending, fmt.Sprintf("%d check%s still running.", pending, plural(pending)), false)
	}
	switch m.ReviewDecision {
	case "CHANGES_REQUESTED":
		add(BlockChangesRequested, "A reviewer requested changes.", true)
	case "REVIEW_REQUIRED":
		add(BlockReviewRequired, "It needs an approving review.", false)
	default:
		// BLOCKED with every reason above clear is branch protection that
		// the API does not itemise, most often a code owner's approval.
		if m.MergeStateStatus == "BLOCKED" && len(out) == 0 {
			add(BlockReviewRequired, "Branch protection is not satisfied yet, most likely a required approval.", false)
		}
	}
	onYou, waiting := 0, 0
	for _, t := range conv.Threads {
		if t.Resolved {
			continue
		}
		if t.Last().Author == viewer {
			waiting++
		} else {
			onYou++
		}
	}
	switch {
	case onYou > 0:
		add(BlockThreads, fmt.Sprintf("%d unresolved thread%s wait%s on your reply.", onYou, plural(onYou), verbS(onYou)), true)
	case waiting > 0:
		add(BlockThreads, fmt.Sprintf("%d unresolved thread%s you answered wait%s on the reviewer.", waiting, plural(waiting), verbS(waiting)), false)
	}
	return out
}

// LaneOf sorts a pull request by whose move it is.
func LaneOf(bs []Blocker) string {
	if len(bs) == 0 {
		return LaneReady
	}
	for _, b := range bs {
		if b.OnYou {
			return LaneNeedsYou
		}
	}
	return LaneWaiting
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func verbS(n int) string {
	if n == 1 {
		return "s"
	}
	return ""
}
