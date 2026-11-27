package demo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/tend"
)

type frame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// ServeAgent is the scripted agent: the agent half of ACP over r and w. For
// a reading prompt it reports believable progress, then writes the sample
// brief through the client's fs/write_text_file, so the same policy checks
// run as with a real agent. For a question it streams a canned answer.
func ServeAgent(r io.Reader, w io.Writer) error {
	step := 700 * time.Millisecond
	if ms, err := strconv.Atoi(os.Getenv("DOCKET_DEMO_STEP_MS")); err == nil {
		step = time.Duration(ms) * time.Millisecond
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	enc := json.NewEncoder(w)
	send := func(f frame) { f.JSONRPC = "2.0"; _ = enc.Encode(f) }
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	nextID := 0
	sessions := 0

	// call sends a request to the client and waits for its answer.
	call := func(method string, params any) (json.RawMessage, string) {
		nextID++
		id := raw(fmt.Sprintf("a%d", nextID))
		send(frame{ID: id, Method: method, Params: raw(params)})
		for sc.Scan() {
			var f frame
			if json.Unmarshal(sc.Bytes(), &f) != nil || f.Method != "" || string(f.ID) != string(id) {
				continue
			}
			if len(f.Error) > 0 {
				var e struct {
					Message string `json:"message"`
				}
				_ = json.Unmarshal(f.Error, &e)
				return nil, e.Message
			}
			return f.Result, ""
		}
		return nil, "client went away"
	}
	update := func(session string, u map[string]any) {
		send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": session, "update": u})})
	}
	say := func(session, text string) {
		update(session, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": text}})
	}
	tool := func(session string, n int, title, kind string) {
		id := fmt.Sprintf("t%d", n)
		update(session, map[string]any{"sessionUpdate": "tool_call", "toolCallId": id, "title": title, "kind": kind, "status": "in_progress"})
		time.Sleep(step)
		update(session, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id, "status": "completed"})
	}

	for sc.Scan() {
		var f frame
		if json.Unmarshal(sc.Bytes(), &f) != nil {
			continue
		}
		switch f.Method {
		case "initialize":
			send(frame{ID: f.ID, Result: raw(map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}, "authMethods": []any{}})})
		case "session/new":
			sessions++
			send(frame{ID: f.ID, Result: raw(map[string]any{
				"sessionId": fmt.Sprintf("demo-%d", sessions),
				"models":    map[string]any{"currentModelId": "scripted", "availableModels": []map[string]string{{"modelId": "scripted", "name": "Scripted demo agent"}}},
			})})
		case "session/prompt":
			var p struct {
				SessionID string `json:"sessionId"`
				Prompt    []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			}
			_ = json.Unmarshal(f.Params, &p)
			var sb strings.Builder
			for _, b := range p.Prompt {
				sb.WriteString(b.Text)
			}
			prompt := sb.String()
			if strings.HasPrefix(prompt, tend.GatePrefix) {
				say(p.SessionID, gateAnswer(prompt))
				send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn"})})
				continue
			}
			if strings.HasPrefix(prompt, tend.PromptPrefix) {
				dir, out := tendDirs(prompt)
				var pr struct {
					Number int `json:"number"`
				}
				if b, err := os.ReadFile(filepath.Join(dir, "pr.json")); err == nil {
					_ = json.Unmarshal(b, &pr)
				}
				for i, s := range []struct{ title, kind string }{
					{"Reading the threads and the failed log", "read"},
					{"Running the failing test", "execute"},
					{"Committing the fixes", "edit"},
					{"Running the tests again", "execute"},
					{"Writing the plan", "edit"},
				} {
					tool(p.SessionID, i+1, s.title, s.kind)
				}
				if body := tendPlan(pr.Number); body == "" {
					say(p.SessionID, "The demo agent has nothing to do on this pull request.")
				} else if _, msg := call("fs/write_text_file", map[string]any{"sessionId": p.SessionID, "path": filepath.Join(out, "plan.json"), "content": body}); msg != "" {
					say(p.SessionID, "Could not write the plan: "+msg)
				}
				send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn"})})
				continue
			}
			if strings.HasPrefix(prompt, "A reviewer is reading") {
				time.Sleep(step)
				tool(p.SessionID, 1, "Searching the three receiver repositories", "search")
				for _, part := range []string{
					"Partly. ",
					"I searched the receivers in this organisation that I can read.\n\n",
					"- `billing-sync` dedupes on the event id (`src/consumer.ts:41`, a unique index on `webhook_id`).\n",
					"- `crm-bridge` and `notifier` do not. `notifier` would send the customer email once per delivery (`src/handlers/payment.ts:18`).\n\n",
					"I cannot see what external customers' endpoints do.",
				} {
					say(p.SessionID, part)
					time.Sleep(step / 3)
				}
				send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn"})})
				continue
			}
			dir, out := dirsFromPrompt(prompt)
			var pr struct {
				Number int `json:"number"`
			}
			if b, err := os.ReadFile(filepath.Join(dir, "pr.json")); err == nil {
				_ = json.Unmarshal(b, &pr)
			}
			if !strings.Contains(prompt, "cannot be used yet") {
				for i, s := range []struct{ title, kind string }{
					{"Reading the diff and the description", "read"},
					{"Installing dependencies in both checkouts", "execute"},
					{"Running the tests on head", "execute"},
					{"Replaying recorded cases on base and head", "execute"},
					{"Writing probes for what the description claims", "edit"},
					{"Checking your rules", "think"},
					{"Writing the brief", "edit"},
				} {
					tool(p.SessionID, i+1, s.title, s.kind)
				}
			}
			body := fixture(fmt.Sprintf("%d.json", pr.Number))
			if body == "" {
				say(p.SessionID, "The demo agent has no brief for this pull request.")
			} else if _, msg := call("fs/write_text_file", map[string]any{"sessionId": p.SessionID, "path": filepath.Join(out, "brief.json"), "content": body}); msg != "" {
				say(p.SessionID, "Could not write the brief: "+msg)
			}
			send(frame{ID: f.ID, Result: raw(map[string]string{"stopReason": "end_turn"})})
		default:
			if len(f.ID) > 0 && f.Method != "" {
				send(frame{ID: f.ID, Error: raw(map[string]any{"code": -32601, "message": "not supported by the demo agent"})})
			}
		}
	}
	return sc.Err()
}

// tendDirs pulls the input and output directories out of a tend prompt.
func tendDirs(prompt string) (dir, out string) {
	for _, ln := range strings.Split(prompt, "\n") {
		if i := strings.Index(ln, "/pr.json"); i >= 0 && dir == "" {
			j := strings.LastIndexAny(ln[:i], "` ")
			dir = ln[j+1 : i]
		}
		if i := strings.Index(ln, "/plan.json"); i >= 0 && out == "" {
			j := strings.LastIndexAny(ln[:i], "` ")
			out = ln[j+1 : i]
		}
	}
	return dir, out
}

func tendPlan(number int) string {
	var p map[string]any
	switch number {
	case 318:
		p = map[string]any{
			"summary": "Renamed curMeta as sam-t asked, and found the test failure: Refresh writes into a map that is nil until the first load. The fix for that is committed; the refresh interval question is yours.",
			"actions": []map[string]any{
				{"id": "a1", "kind": "fix", "commit": commitRename, "threads": []string{"T_318_1"}, "why": "Renames curMeta to currencyMeta in its two uses. go build and go vet pass."},
				{"id": "a2", "kind": "reply", "thread": "T_318_1", "stance": "agrees", "body": "Renamed to `currencyMeta`.", "why": "Says the rename is done."},
				{"id": "a3", "kind": "resolve", "thread": "T_318_1", "why": "The rename answers the thread fully."},
				{"id": "a4", "kind": "fix", "commit": commitNilMap, "checks": []string{"CR_test"}, "why": "TestSettleUsesCachedMeta panics because Refresh writes into currencyMeta before Load has made it. Refresh now makes the map when it is nil. The test passes locally."},
				{"id": "a5", "kind": "reply", "thread": "T_318_2", "stance": "asks", "body": "The interval is hourly for now. Making it configurable means a new setting in the ops config: in this PR, or a follow-up?", "why": "Making it configurable is a design choice, so this asks instead of changing code."},
			},
		}
	case 2230:
		p = map[string]any{
			"summary": "main renamed DatePicker's zone handling while this moved the helpers. I merged main into the branch and kept both changes.",
			"actions": []map[string]any{
				{"id": "a1", "kind": "merge-base", "commit": commitMerge, "why": "One conflict, in DatePicker.tsx: kept the new import path from this branch and the UTC default from main. The tests pass."},
			},
		}
	default:
		return ""
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	return string(b)
}

// gateAnswer is the scripted classifier: a rename and the notes about it run
// on their own; a change to how the cache starts goes to the author.
func gateAnswer(prompt string) string {
	switch {
	case strings.Contains(prompt, "make(map"):
		return `{"decision": "ask", "reason": "It changes how the cache starts up, not only something a reviewer named. Worth a look."}`
	case strings.Contains(prompt, "kind: reply"):
		return `{"decision": "auto", "reason": "A short note that the requested rename was made."}`
	case strings.Contains(prompt, "kind: resolve"):
		return `{"decision": "auto", "reason": "The rename answers the thread fully."}`
	default:
		return `{"decision": "auto", "reason": "A rename the reviewer asked for, and nothing else."}`
	}
}
