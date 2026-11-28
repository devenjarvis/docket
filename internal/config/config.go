// Package config finds Docket's home directory and reads its settings.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/devenjarvis/docket/internal/loginenv"
)

// Config is ~/.docket/config.json.
type Config struct {
	// Agent is the id of a built-in agent (claude, codex, …).
	Agent string `json:"agent,omitempty"`
	// AgentCommand overrides the built-in command for that agent.
	AgentCommand string `json:"agent_command,omitempty"`
	// AgentPath lists extra directories to search for the agent.
	AgentPath []string `json:"agent_path,omitempty"`
	// PollSeconds is how often to ask GitHub for assigned PRs.
	PollSeconds int `json:"poll_seconds,omitempty"`
	// Concurrency is how many PRs an agent reads at once.
	Concurrency int `json:"concurrency,omitempty"`
	// Theme is light, dark or auto. The app keeps its own copy in the
	// webview's storage, with the colourway.
	Theme string `json:"theme,omitempty"`
	// ReceiptInReview adds the attention receipt to the posted review body.
	ReceiptInReview *bool `json:"receipt_in_review,omitempty"`
	// RunCode lets the agent execute the pull request's code in its
	// throwaway checkouts. On unless set to false.
	RunCode *bool `json:"run_code,omitempty"`
	// Tend is how the agent works on the user's own pull requests: auto
	// (on its own when the gate allows), ask (every step waits) or off.
	Tend string `json:"tend,omitempty"`
	// SelfReview is whether the agent reads the user's own pull requests the
	// way it reads others': auto (every new head), ask (when the user asks)
	// or off.
	SelfReview string `json:"self_review,omitempty"`
	// Strategy is how a pull request is read: single, panel, swarm or
	// critic (see the Strategy* constants).
	Strategy string `json:"strategy,omitempty"`
	// Instructions are the user's standing instructions, given to every
	// reading session after Docket's own prompt.
	Instructions string `json:"instructions,omitempty"`
	// Repos holds what the user set for one repository (owner/name),
	// overriding the settings above.
	Repos map[string]Repo `json:"repos,omitempty"`
	// NotifyOff lists the kinds of desktop notification the user turned
	// off (see the Notify* constants).
	NotifyOff []string `json:"notify_off,omitempty"`
	// Prices are what each model costs, used to estimate a cost when the
	// agent reports tokens but no cost. Keyed by the model's name as the
	// agent reports it.
	Prices map[string]Price `json:"prices,omitempty"`
}

// Repo is the user's settings for one repository.
type Repo struct {
	// Off makes Docket leave the repository alone: its pull requests are not
	// read, listed or worked on.
	Off bool `json:"off,omitempty"`
	// RunCode, when set, overrides Config.RunCode here.
	RunCode *bool `json:"run_code,omitempty"`
	// Tend, when set (auto, ask or off), overrides Config.Tend here.
	Tend string `json:"tend,omitempty"`
	// SelfReview, when set (auto, ask or off), overrides Config.SelfReview.
	SelfReview string `json:"self_review,omitempty"`
	// Strategy, when set, overrides Config.Strategy here.
	Strategy string `json:"strategy,omitempty"`
}

// Price is a model's price in US dollars per million tokens.
type Price struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

// Kinds of desktop notification the user can turn off.
const (
	NotifyRead     = "read"     // a pull request was read and waits on the user
	NotifyLane     = "lane"     // one of theirs became ready to merge, or started to need them
	NotifyActivity = "activity" // someone reviewed or commented on one of theirs
	NotifyTend     = "tend"     // the agent did steps on one of theirs, or holds one for them
)

// NotifyKinds lists every kind, in the order Settings shows them.
var NotifyKinds = []string{NotifyRead, NotifyLane, NotifyActivity, NotifyTend}

// Notifies reports whether notifications of kind are on.
func (c Config) Notifies(kind string) bool {
	for _, k := range c.NotifyOff {
		if k == kind {
			return false
		}
	}
	return true
}

// RepoOff reports whether the user turned Docket off for repo (owner/name).
func (c Config) RepoOff(repo string) bool { return c.Repos[repo].Off }

// RunsIn reports whether the agent may execute code from repo.
func (c Config) RunsIn(repo string) bool {
	if r, ok := c.Repos[repo]; ok && r.RunCode != nil {
		return *r.RunCode
	}
	return c.Runs()
}

// TendIn is the tend mode for repo: its own when set, else TendMode.
func (c Config) TendIn(repo string) string {
	switch t := c.Repos[repo].Tend; t {
	case "auto", "ask", "off":
		return t
	}
	return c.TendMode()
}

// TendMode is Tend with its default, auto.
func (c Config) TendMode() string {
	switch c.Tend {
	case "ask", "off":
		return c.Tend
	}
	return "auto"
}

// SelfIn is the self-review mode for repo: its own when set, else SelfMode.
func (c Config) SelfIn(repo string) string {
	switch t := c.Repos[repo].SelfReview; t {
	case "auto", "ask", "off":
		return t
	}
	return c.SelfMode()
}

// SelfMode is SelfReview with its default, auto.
func (c Config) SelfMode() string {
	switch c.SelfReview {
	case "ask", "off":
		return c.SelfReview
	}
	return "auto"
}

// How a pull request is read.
const (
	// StrategySingle is one session of the agent's own model, reading the
	// whole change and writing the brief.
	StrategySingle = "single"
	// StrategyPanel runs the lenses first, each a read-only session on the
	// agent's smallest model, then one session of the agent's own model
	// checks what they found and writes the brief.
	StrategyPanel = "panel"
	// StrategySwarm is the panel with the brief written on the smallest
	// model too: every session is a small one.
	StrategySwarm = "swarm"
	// StrategyCritic is one read, then a second session of the same model
	// that tries to knock down each finding and claim and rewrites the brief
	// without the ones that fall.
	StrategyCritic = "critic"
)

// Strategies lists every strategy, in the order Settings shows them.
var Strategies = []string{StrategySingle, StrategyPanel, StrategySwarm, StrategyCritic}

// ValidStrategy reports whether s names a strategy.
func ValidStrategy(s string) bool {
	for _, v := range Strategies {
		if s == v {
			return true
		}
	}
	return false
}

// StrategyMode is Strategy with its default, single.
func (c Config) StrategyMode() string {
	if ValidStrategy(c.Strategy) {
		return c.Strategy
	}
	return StrategySingle
}

// StrategyIn is the strategy for repo: its own when set, else StrategyMode.
func (c Config) StrategyIn(repo string) string {
	if s := c.Repos[repo].Strategy; ValidStrategy(s) {
		return s
	}
	return c.StrategyMode()
}

// MaxInstructions bounds the user's standing instructions.
const MaxInstructions = 4000

func (c Config) Poll() int {
	if c.PollSeconds < 30 {
		return 120
	}
	return c.PollSeconds
}

func (c Config) Workers() int {
	if c.Concurrency < 1 {
		return 2
	}
	if c.Concurrency > 6 {
		return 6
	}
	return c.Concurrency
}

// Runs reports whether the agent may execute code from the pull request.
func (c Config) Runs() bool { return c.RunCode == nil || *c.RunCode }

func (c Config) Receipt() bool { return c.ReceiptInReview == nil || *c.ReceiptInReview }

// Home is Docket's data directory: $DOCKET_HOME, else ~/.docket.
func Home() (string, error) {
	if h := os.Getenv("DOCKET_HOME"); h != "" {
		return h, os.MkdirAll(h, 0o755)
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	h := filepath.Join(u, ".docket")
	return h, os.MkdirAll(h, 0o755)
}

var mu sync.Mutex

func path() (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "config.json"), nil
}

// Read returns the saved settings, or zero settings when none are saved.
func Read() (Config, error) {
	mu.Lock()
	defer mu.Unlock()
	var c Config
	p, err := path()
	if err != nil {
		return c, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

// Write saves the settings.
func Write(c Config) error {
	mu.Lock()
	defer mu.Unlock()
	p, err := path()
	if err != nil {
		return err
	}
	return WriteJSON(p, c)
}

// WriteJSON writes v to path through a temporary file, so a crash never
// leaves half a file.
func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func init() {
	loginenv.ConfiguredPath = func() []string {
		c, _ := Read()
		if c.AgentPath == nil {
			return []string{}
		}
		return c.AgentPath
	}
}
