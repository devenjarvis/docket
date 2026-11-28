package model

import "time"

// Tend action kinds: what the agent may propose to move an own pull request
// along.
const (
	ActFix          = "fix"           // a commit on top of the head
	ActReply        = "reply"         // a reply in a review thread
	ActResolve      = "resolve"       // resolve a review thread
	ActUpdateBranch = "update-branch" // merge the base into the branch on GitHub
	ActRerun        = "rerun"         // re-run a failed check's jobs
	ActMergeBase    = "merge-base"    // a commit that merges the base and resolves conflicts
)

// Gate decisions.
const (
	GateAuto = "auto"
	GateAsk  = "ask"
)

// Tend action states.
const (
	StateWaiting  = "waiting"  // an ask the user has not answered
	StateWorking  = "working"  // being carried out or undone right now
	StateDone     = "done"     // carried out
	StateRejected = "rejected" // the user sent it back
	StateUndone   = "undone"   // carried out, then undone
	StateFailed   = "failed"   // carrying it out failed
	StateSkipped  = "skipped"  // could not run, because one it depends on did not
)

// TendPlan is what the agent writes to plan.json.
type TendPlan struct {
	Summary string       `json:"summary"`
	Actions []TendAction `json:"actions"`
}

// TendAction is one step the agent proposes, with what Docket decided and
// did about it.
type TendAction struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Commit is the fix or merge commit, made in the branch checkout.
	Commit string `json:"commit,omitempty"`
	// Thread is the review thread a reply or resolve is for.
	Thread string `json:"thread,omitempty"`
	// Threads and Checks are what a fix answers, or the check a rerun is for.
	Threads []string `json:"threads,omitempty"`
	Checks  []string `json:"checks,omitempty"`
	// Tasks are the self-review findings a fix answers, by decision id.
	Tasks []string `json:"tasks,omitempty"`
	// Body is a reply's text.
	Body string `json:"body,omitempty"`
	// Stance says how a reply meets the reviewer's request: agrees,
	// informs, asks or declines.
	Stance string `json:"stance,omitempty"`
	// Why is the agent's reason, in one or two sentences.
	Why string `json:"why"`

	// The rest is Docket's, not the agent's.
	Verdict *Verdict  `json:"verdict,omitempty"`
	State   string    `json:"state,omitempty"`
	Result  string    `json:"result,omitempty"`
	Patch   string    `json:"patch,omitempty"`
	At      time.Time `json:"at,omitempty"`
	// Undo is what reverses it: a revert commit's parent tip, or a reply's
	// comment id. Empty means it cannot be undone.
	Undo string `json:"undo,omitempty"`
}

// Verdict is the gate's call on one action.
type Verdict struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	// Floor is true when a fixed rule decided, not the classifier.
	Floor bool `json:"floor,omitempty"`
	// Model is the model the classifier ran on, when it ran.
	Model string `json:"model,omitempty"`
}

// Tend is the agent's latest run on an own pull request.
type Tend struct {
	// Status is running, ready (the plan is in and acted on), failed or idle.
	Status   string `json:"status"`
	Activity string `json:"activity,omitempty"`
	Error    string `json:"error,omitempty"`
	// Head is the head SHA the run began at; Tip is where the branch is now
	// after the fixes Docket pushed.
	Head    string       `json:"head"`
	Tip     string       `json:"tip,omitempty"`
	Summary string       `json:"summary,omitempty"`
	Actions []TendAction `json:"actions,omitempty"`
	// Run counts the runs on this pull request, from 1.
	Run int `json:"run,omitempty"`
	// Earlier holds what earlier runs did, newest last, with ids prefixed by
	// their run ("2.a1"), so their Undo still works.
	Earlier []TendAction `json:"earlier,omitempty"`
	// Trigger names what started it: the blockers, in words.
	Trigger string    `json:"trigger,omitempty"`
	At      time.Time `json:"at"`
	// Notes are what the user sent back, for the next run.
	Notes []string `json:"notes,omitempty"`
	// Tasks are self-review findings the author asked the agent to fix.
	Tasks []TendTask `json:"tasks,omitempty"`
	// Agent and Model name who did it.
	Agent string `json:"agent,omitempty"`
	Model string `json:"model,omitempty"`
	// Branch is the checkout the agent committed in, kept so a waiting fix
	// can still be pushed and a pushed one reverted.
	Branch TendBranch `json:"branch"`
}

// TendTask is one self-review finding the author handed to the agent: what
// is wrong, where, and what the author wants done about it.
type TendTask struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Path   string `json:"path,omitempty"`
	Line   int    `json:"line,omitempty"`
	// Ask is the fix the author chose, in their words or the agent's draft.
	Ask string `json:"ask"`
}

// TendBranch is where a tend run's checkout lives (repo.Branch, which this
// package cannot import).
type TendBranch struct {
	Dir     string `json:"dir"`
	Work    string `json:"work"`
	Scratch string `json:"scratch"`
	Out     string `json:"out"`
	Start   string `json:"start"`
	Remote  string `json:"remote"`
	GitDir  string `json:"gitDir"`
}
