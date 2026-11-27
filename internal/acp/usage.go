package acp

import (
	"encoding/json"
	"sort"
)

// Tokens is token usage as an agent reports it for prompt turns. The shape
// follows ACP's end-turn token usage RFD (a draft): a `usage` object on the
// session/prompt response, or on a state_update that ends a turn. Each
// report is taken as one turn's usage and added up.
type Tokens struct {
	Total      int64 `json:"totalTokens"`
	Input      int64 `json:"inputTokens"`
	Output     int64 `json:"outputTokens"`
	Thought    int64 `json:"thoughtTokens,omitempty"`
	CacheRead  int64 `json:"cachedReadTokens,omitempty"`
	CacheWrite int64 `json:"cachedWriteTokens,omitempty"`
}

func (t *Tokens) add(o Tokens) {
	if o.Total == 0 {
		o.Total = o.Input + o.Output + o.Thought
	}
	t.Total += o.Total
	t.Input += o.Input
	t.Output += o.Output
	t.Thought += o.Thought
	t.CacheRead += o.CacheRead
	t.CacheWrite += o.CacheWrite
}

// Usage is what one model used on one client: the tokens of every turn the
// agent reported, and the cost the agent reported (ACP's usage_update, whose
// cost is the session's running total).
type Usage struct {
	Model string `json:"model"`
	Tokens
	// Turns is how many turns reported tokens; zero means the agent
	// reported none and Tokens is empty.
	Turns int `json:"turns,omitempty"`
	// Cost is the agent's own figure, in Currency; HasCost says whether it
	// sent one at all.
	Cost     float64 `json:"cost,omitempty"`
	Currency string  `json:"currency,omitempty"`
	HasCost  bool    `json:"hasCost,omitempty"`
}

type sessionUsage struct {
	model    string
	tokens   Tokens
	turns    int
	cost     float64
	currency string
	hasCost  bool
}

func (c *Client) usageFor(sessionID string) *sessionUsage {
	if c.usage == nil {
		c.usage = map[string]*sessionUsage{}
	}
	u := c.usage[sessionID]
	if u == nil {
		u = &sessionUsage{model: c.model}
		c.usage[sessionID] = u
	}
	return u
}

// noteTokens adds one turn's reported tokens to the session.
func (c *Client) noteTokens(sessionID string, raw json.RawMessage) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	var t Tokens
	if json.Unmarshal(raw, &t) != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	u := c.usageFor(sessionID)
	u.tokens.add(t)
	u.turns++
}

// noteUpdate reads the usage an update carries: usage_update's cumulative
// cost, and the token usage a turn-ending state_update may carry.
func (c *Client) noteUpdate(sessionID string, raw json.RawMessage) {
	var p struct {
		Kind string `json:"sessionUpdate"`
		Cost *struct {
			Amount   float64 `json:"amount"`
			Currency string  `json:"currency"`
		} `json:"cost"`
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	switch p.Kind {
	case "usage_update":
		if p.Cost == nil {
			return
		}
		c.mu.Lock()
		u := c.usageFor(sessionID)
		u.cost, u.currency, u.hasCost = p.Cost.Amount, p.Cost.Currency, true
		c.mu.Unlock()
	case "state_update":
		c.noteTokens(sessionID, p.Usage)
	}
}

// Usage returns what each model used on this client, across its sessions,
// sorted by model.
func (c *Client) Usage() []Usage {
	c.mu.Lock()
	defer c.mu.Unlock()
	by := map[string]*Usage{}
	for _, s := range c.usage {
		if s.turns == 0 && !s.hasCost {
			continue
		}
		u := by[s.model]
		if u == nil {
			u = &Usage{Model: s.model}
			by[s.model] = u
		}
		u.Tokens.add(s.tokens)
		u.Turns += s.turns
		if s.hasCost {
			if u.HasCost && u.Currency != s.currency {
				// Mixed currencies cannot be added; keep the first.
				continue
			}
			u.Cost += s.cost
			u.Currency, u.HasCost = s.currency, true
		}
	}
	out := make([]Usage, 0, len(by))
	for _, u := range by {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}
