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
	// Theme is whiteprint, blueprint or auto.
	Theme string `json:"theme,omitempty"`
	// ReceiptInReview adds the attention receipt to the posted review body.
	ReceiptInReview *bool `json:"receipt_in_review,omitempty"`
	// RunCode lets the agent execute the pull request's code in its
	// throwaway checkouts. On unless set to false.
	RunCode *bool `json:"run_code,omitempty"`
	// Tend is how the agent works on the user's own pull requests: auto
	// (on its own when the gate allows), ask (every step waits) or off.
	Tend string `json:"tend,omitempty"`
}

// TendMode is Tend with its default, auto.
func (c Config) TendMode() string {
	switch c.Tend {
	case "ask", "off":
		return c.Tend
	}
	return "auto"
}

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
