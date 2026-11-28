// Package model holds the types Docket stores and sends to the UI: a pull
// request as GitHub describes it, the brief an agent writes about it, and the
// reviewer's own state (decisions, comments, time spent).
package model

import "time"

// PR is one pull request the user is asked to look at.
type PR struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Author string `json:"author"`
	URL    string `json:"url"`
	// HeadSHA and BaseSHA pin the exact change that was read. A new HeadSHA
	// means the author pushed, and the brief is out of date.
	HeadSHA   string `json:"headSha"`
	BaseSHA   string `json:"baseSha"`
	BaseRef   string `json:"baseRef"`
	HeadRef   string `json:"headRef"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Files     int    `json:"files"`
	Draft     bool   `json:"draft"`
	// State is "open", "closed" or "merged". Empty means open.
	State     string    `json:"state,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Why says how the PR reached the user: "review requested", "assigned",
	// "team: platform", "added by hand".
	Why []string `json:"why"`
}

// Key is the PR's stable id: owner/repo#number.
func (p PR) Key() string { return Key(p.Owner, p.Repo, p.Number) }

// Brief is what the agent hands back after reading a PR. Every field is
// evidence or a question; none of it is a verdict.
type Brief struct {
	// Summary says what the change does in two or three plain sentences.
	Summary string `json:"summary"`
	// Claims are the things the PR says about itself (and the things it
	// changes without saying), each checked.
	Claims []Claim `json:"claims"`
	// Sections re-cut the diff by intent, in reading order.
	Sections []Section `json:"sections"`
	// Decisions are the questions only the reviewer can answer.
	Decisions []Decision `json:"decisions"`
	// Context is what the reviewer needs to know before judging.
	Context *Context `json:"context,omitempty"`
	// RuleHits says what each of the reviewer's rules found.
	RuleHits []RuleHit `json:"ruleHits"`
	// Uncovered lists kinds of risk nothing here looked at.
	Uncovered []string `json:"uncovered"`
	// Work is the agent's own log: what it ran and what came back.
	Work []WorkItem `json:"work"`
	// Since describes what changed after an earlier reading, when the author
	// pushed again.
	Since *Since `json:"since,omitempty"`
}

// Claim statuses.
const (
	Proved       = "proved"
	Contradicted = "contradicted"
	Unproven     = "unproven"
	Unstated     = "unstated"
)

type Claim struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
	// Decision links a contradicted or unproven claim to the decision that
	// settles it.
	Decision string `json:"decision,omitempty"`
}

// Section kinds.
const (
	Judgment   = "judgment"
	Skim       = "skim"
	Mechanical = "mechanical"
)

type Section struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
	// Why says, in one sentence, why this section is this kind.
	Why string `json:"why"`
	// Proof, for skim and mechanical sections, is how the agent verified it
	// (tests run, syntax trees compared, regenerated and matched).
	Proof   string      `json:"proof,omitempty"`
	Files   []FileRange `json:"files"`
	Minutes int         `json:"minutes"`
	// Lines is filled in by Docket from the diff, not by the agent.
	Lines int `json:"lines"`
}

// FileRange names part of one file's diff. Ranges are line numbers on the new
// side; an empty Ranges means the whole file's diff.
type FileRange struct {
	Path   string   `json:"path"`
	Ranges [][2]int `json:"ranges,omitempty"`
}

type Decision struct {
	ID      string `json:"id"`
	Section string `json:"section"`
	// Title states the finding as a fact.
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// Question is what the reviewer must decide, phrased as a question.
	Question string   `json:"question"`
	Options  []Option `json:"options"`
	// Anchor is where a comment about this decision attaches.
	Anchor Anchor `json:"anchor"`
	// Hot marks the lines that matter, so the diff can show only those.
	Hot         []FileRange  `json:"hot"`
	Evidence    []Evidence   `json:"evidence"`
	BeforeAfter *BeforeAfter `json:"beforeAfter,omitempty"`
	// Rule is the id of the reviewer's rule that raised this, if one did.
	Rule string `json:"rule,omitempty"`
	// ProposedRule is a rule the reviewer might want, when none raised this.
	ProposedRule *ProposedRule `json:"proposedRule,omitempty"`
}

// Option effects.
const (
	Blocking = "blocking"
	Accept   = "accept"
	Ask      = "ask"
)

type Option struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
	Effect string `json:"effect"`
	// Comment is the draft review comment this choice would post.
	Comment string `json:"comment"`
}

type Anchor struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

// Evidence is something the agent ran, with what came back.
type Evidence struct {
	// Kind is probe (a test the agent wrote), run (an existing command),
	// search (looked through code) or read.
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Command string `json:"command,omitempty"`
	Code    string `json:"code,omitempty"`
	Output  string `json:"output,omitempty"`
	// Passed is nil when pass/fail does not apply.
	Passed *bool `json:"passed,omitempty"`
	// File is the probe's path under the review's out directory, so the
	// reviewer can rerun it.
	File string `json:"file,omitempty"`
}

type ProposedRule struct {
	Text    string `json:"text"`
	Applies string `json:"applies"`
}

// BeforeAfter shows the same scenario on the base and on the head. Kind is
// timeline, table, text or image.
type BeforeAfter struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Scenario string `json:"scenario"`
	// Measured is true when the agent ran both sides, false when it reasoned
	// from the code.
	Measured bool `json:"measured"`

	// timeline
	Unit   string     `json:"unit,omitempty"`
	Max    float64    `json:"max,omitempty"`
	Before *Timeline  `json:"before,omitempty"`
	After  *Timeline  `json:"after,omitempty"`
	Rows   []TableRow `json:"rows,omitempty"` // table
	// text and image: the two sides as text, or as paths under out/.
	BeforeText string `json:"beforeText,omitempty"`
	AfterText  string `json:"afterText,omitempty"`
}

type Timeline struct {
	Summary string `json:"summary"`
	Lanes   []Lane `json:"lanes"`
}

type Lane struct {
	Label string `json:"label"`
	Spans []Span `json:"spans,omitempty"`
	Marks []Mark `json:"marks,omitempty"`
}

// Span kinds: work (something in flight) or wait.
type Span struct {
	From  float64 `json:"from"`
	To    float64 `json:"to"`
	Label string  `json:"label,omitempty"`
	Kind  string  `json:"kind,omitempty"`
}

type Mark struct {
	At    float64 `json:"at"`
	Label string  `json:"label,omitempty"`
}

type TableRow struct {
	Label   string `json:"label"`
	Before  string `json:"before"`
	After   string `json:"after"`
	Changed bool   `json:"changed"`
}

// Context is the page a reviewer reads first when the code is unfamiliar.
type Context struct {
	Title string `json:"title"`
	Lede  string `json:"lede"`
	// Path is the chain of steps the change sits on.
	Path []PathStep `json:"path"`
	// Facts are the few things worth holding in mind, each with a source.
	Facts       []Fact       `json:"facts"`
	BeforeAfter *BeforeAfter `json:"beforeAfter,omitempty"`
	// Analogy relates the change to code the reviewer already knows.
	Analogy *Fact    `json:"analogy,omitempty"`
	Sources []Source `json:"sources"`
}

type PathStep struct {
	Label   string `json:"label"`
	Note    string `json:"note"`
	Changed bool   `json:"changed"`
}

type Fact struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

type Source struct {
	Ref  string `json:"ref"`
	Note string `json:"note,omitempty"`
}

type RuleHit struct {
	Rule string `json:"rule"`
	// Result is violated, passed or unclear.
	Result   string `json:"result"`
	Note     string `json:"note"`
	Decision string `json:"decision,omitempty"`
}

type WorkItem struct {
	Did    string `json:"did"`
	Result string `json:"result"`
}

type Since struct {
	Summary  string   `json:"summary"`
	Resolved []string `json:"resolved"`
	New      []string `json:"new"`
}

// Review is everything Docket keeps about one PR.
type Review struct {
	PR PR `json:"pr"`
	// Status is queued, reading, ready, failed or posted.
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	// ReadSHA is the head the brief was written against.
	ReadSHA string `json:"readSha,omitempty"`
	Brief   *Brief `json:"brief,omitempty"`
	// Slice lists the changed files that fall to this reviewer, when
	// CODEOWNERS narrows it; empty means all of them.
	Slice []string `json:"slice,omitempty"`
	// Answers holds the reviewer's choice for each decision id.
	Answers map[string]Answer `json:"answers"`
	// Seconds is active time per section or decision id.
	Seconds map[string]int `json:"seconds"`
	// Notes are free comments the reviewer added by hand.
	Notes []Note `json:"notes"`
	// Asks are follow-up questions and the agent's answers.
	Asks []AskTurn `json:"asks"`
	// Audited lists mechanical sections the reviewer spot-checked.
	Audited map[string]bool `json:"audited"`
	Posted  *Posted         `json:"posted,omitempty"`
	Agent   string          `json:"agent,omitempty"`
	Model   string          `json:"model,omitempty"`
	ReadAt  time.Time       `json:"readAt,omitempty"`
	// FoundAt is when Docket first saw the review request: the start of
	// the author's wait on this reviewer.
	FoundAt  time.Time `json:"foundAt,omitempty"`
	ReadTook int       `json:"readTookSeconds,omitempty"`
	// Activity is the agent's latest progress line while it reads.
	Activity string `json:"activity,omitempty"`
	// Isolation says whether the agent kept to Docket's file policy.
	Isolation string `json:"isolation,omitempty"`
}

type Answer struct {
	Option string `json:"option"`
	// Comment is the draft as the reviewer left it; empty means use the
	// option's own draft.
	Comment string `json:"comment,omitempty"`
	// Feedback is the reviewer's verdict on the rule that raised this:
	// useful, not-here or stop.
	Feedback string    `json:"feedback,omitempty"`
	At       time.Time `json:"at"`
}

type Note struct {
	ID   string `json:"id"`
	Path string `json:"path,omitempty"`
	Line int    `json:"line,omitempty"`
	Body string `json:"body"`
}

type AskTurn struct {
	ID       string    `json:"id"`
	Decision string    `json:"decision,omitempty"`
	Question string    `json:"question"`
	Answer   string    `json:"answer"`
	Pending  bool      `json:"pending"`
	At       time.Time `json:"at"`
}

type Posted struct {
	Event string    `json:"event"`
	URL   string    `json:"url"`
	At    time.Time `json:"at"`
	SHA   string    `json:"sha"`
}

// Rule is one line of the reviewer's rulebook.
type Rule struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	// Applies names the kind of code the rule is about ("HTTP clients",
	// "SQL migrations"); empty means any code.
	Applies string `json:"applies"`
	// Scope is "everywhere" or a repo as owner/name.
	Scope string `json:"scope"`
	// Except lists repos where the reviewer said the rule does not hold.
	Except []string `json:"except,omitempty"`
	// Severity is blocking, question or note: how a finding is posted.
	Severity string `json:"severity"`
	// Status is active, trial or retired.
	Status string `json:"status"`
	// Origin says where the rule came from, in words.
	Origin  string    `json:"origin"`
	Created time.Time `json:"created"`
	Stats   RuleStats `json:"stats"`
}

type RuleStats struct {
	Fired    int `json:"fired"`
	Agreed   int `json:"agreed"`
	Overrode int `json:"overrode"`
}

// Fading reports whether the reviewer waves the rule off more than they keep
// it, over enough firings to mean something.
func (r Rule) Fading() bool {
	n := r.Stats.Agreed + r.Stats.Overrode
	return n >= 5 && r.Stats.Overrode*10 >= n*6
}
