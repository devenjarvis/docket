// Package ledger keeps Docket's history: one line per thing that is over (a
// read, a question to the agent, a review posted, a run on the user's own
// pull request, a step's outcome), so Settings can show what Docket did and
// what it cost after the reviews themselves are gone from the queue.
//
// The ledger is ~/.docket/ledger.jsonl. Lines are only ever appended; a line
// that does not parse is skipped.
package ledger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/devenjarvis/docket/internal/acp"
)

// Kinds of entry.
const (
	KindRead = "read" // the agent read a pull request to review
	KindAsk  = "ask"  // the reviewer asked the agent a follow-up question
	KindPost = "post" // the reviewer posted a review
	KindTend = "tend" // the agent planned work on the user's own pull request
	KindStep = "step" // one planned step reached an outcome
)

// Outcomes of a step on the user's own pull request.
const (
	StepAuto     = "auto"     // went ahead on its own
	StepApproved = "approved" // held, then pushed or posted by the user
	StepRejected = "rejected" // held, then sent back
	StepUndone   = "undone"   // carried out, then undone by the user
)

// Entry is one line of the ledger. Which fields are set depends on Kind.
type Entry struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Repo   string    `json:"repo"`
	Number int       `json:"number,omitempty"`

	// The agent's work: read, ask, tend.
	Agent   string      `json:"agent,omitempty"`
	Model   string      `json:"model,omitempty"`
	Seconds int         `json:"seconds,omitempty"`
	Failed  bool        `json:"failed,omitempty"`
	Usage   []acp.Usage `json:"usage,omitempty"`

	// read
	Tries        int    `json:"tries,omitempty"`
	Reread       bool   `json:"reread,omitempty"`
	Carried      int    `json:"carried,omitempty"`
	Proved       int    `json:"proved,omitempty"`
	Contradicted int    `json:"contradicted,omitempty"`
	Unproven     int    `json:"unproven,omitempty"`
	Isolation    string `json:"isolation,omitempty"`

	// read and post
	NeedLines  int `json:"needLines,omitempty"`
	TotalLines int `json:"totalLines,omitempty"`
	Decisions  int `json:"decisions,omitempty"`
	Mechanical int `json:"mechanical,omitempty"`

	// post
	Event       string         `json:"event,omitempty"`
	YourSeconds int            `json:"yourSeconds,omitempty"`
	Time        map[string]int `json:"time,omitempty"` // brief, context, decisions, diff
	Answered    int            `json:"answered,omitempty"`
	Edited      int            `json:"edited,omitempty"`
	Blocking    int            `json:"blocking,omitempty"`
	Asks        int            `json:"asks,omitempty"`
	Audited     int            `json:"audited,omitempty"`
	Receipt     bool           `json:"receipt,omitempty"`

	// tend
	Auto  bool `json:"auto,omitempty"`
	Steps int  `json:"steps,omitempty"`
	// Stopped says Docket stopped starting runs on its own after this one.
	Stopped bool `json:"stopped,omitempty"`

	// step
	Step    string `json:"step,omitempty"` // the action's kind
	Outcome string `json:"outcome,omitempty"`
	// Floor says a fixed rule held the step, not the gate's judgment.
	Floor bool `json:"floor,omitempty"`
}

// Ledger is the file. Its methods are safe for concurrent use.
type Ledger struct {
	Path string
	mu   sync.Mutex
}

// Open returns the ledger in home.
func Open(home string) *Ledger { return &Ledger{Path: filepath.Join(home, "ledger.jsonl")} }

// Add appends e, stamping it with the time when it has none.
func (l *Ledger) Add(e Entry) error {
	if l == nil {
		return nil
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// All returns every entry, oldest first as written.
func (l *Ledger) All() ([]Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, err := os.ReadFile(l.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil && !e.At.IsZero() {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// Raw returns the file as it is, or nothing when there is none.
func (l *Ledger) Raw() ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, err := os.ReadFile(l.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// Clear removes every entry.
func (l *Ledger) Clear() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	err := os.Remove(l.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
