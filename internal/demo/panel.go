package demo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// lensFile pulls the findings file a lens prompt names.
func lensFile(prompt string) string {
	for _, ln := range strings.Split(prompt, "\n") {
		if i := strings.Index(ln, "/findings.json`"); i >= 0 {
			j := strings.LastIndex(ln[:i], "`")
			return ln[j+1 : i+len("/findings.json")]
		}
	}
	return ""
}

// firstChanged is the first file the diff changes, for a lead to point at.
func firstChanged(prompt string) string {
	dir, _ := dirsFromPrompt(prompt)
	b, err := os.ReadFile(filepath.Join(dir, "diff.patch"))
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if p, ok := strings.CutPrefix(ln, "+++ b/"); ok {
			return p
		}
	}
	return ""
}

// lensFindings is what a scripted lens hands back: a lead or two from the
// lenses most changes give something to, nothing from the rest.
func lensFindings(id, prompt string) string {
	path := firstChanged(prompt)
	type lead struct {
		Title    string `json:"title"`
		Detail   string `json:"detail"`
		Path     string `json:"path"`
		Line     int    `json:"line"`
		Severity string `json:"severity"`
		Evidence string `json:"evidence"`
	}
	var leads []lead
	switch id {
	case "correctness":
		leads = []lead{
			{"A retry can repeat work that already succeeded.", "When the second attempt starts after the first one wrote its result, the result is written twice.", path, 1, "high", path + ":1"},
			{"An empty input is not handled.", "The loop assumes at least one element.", path, 1, "low", path + ":1"},
		}
	case "claims":
		leads = []lead{{"The description leaves out a changed default.", "The change alters behaviour the description does not mention.", path, 1, "medium", path + ":1"}}
	case "tests":
		leads = []lead{{"The changed path has no test.", "No test exercises the new branch.", path, 1, "medium", path + ":1"}}
	}
	if leads == nil {
		leads = []lead{}
	}
	b, _ := json.MarshalIndent(map[string]any{"findings": leads}, "", "  ")
	return string(b)
}

// credit marks each decision in a fixture brief with a lens that found
// leads, the way a lead reader working from a panel does, and logs a lead
// it dropped.
func credit(body, lensesPath string) string {
	var reports []struct {
		Lens     string            `json:"lens"`
		Name     string            `json:"name"`
		Findings []json.RawMessage `json:"findings"`
	}
	b, err := os.ReadFile(lensesPath)
	if err != nil || json.Unmarshal(b, &reports) != nil {
		return body
	}
	var ids []string
	leads := 0
	for _, r := range reports {
		if len(r.Findings) > 0 {
			ids = append(ids, r.Lens)
		}
		leads += len(r.Findings)
	}
	var br map[string]any
	if len(ids) == 0 || json.Unmarshal([]byte(body), &br) != nil {
		return body
	}
	decs, _ := br["decisions"].([]any)
	for i, d := range decs {
		if m, ok := d.(map[string]any); ok {
			m["lens"] = ids[i%len(ids)]
		}
	}
	if leads > len(decs) {
		work, _ := br["work"].([]any)
		br["work"] = append(work, map[string]any{"did": "Dropped lead from " + ids[0] + ": An empty input is not handled.", "result": "The caller never passes an empty list; the guard is one frame up."})
	}
	out, err := json.MarshalIndent(br, "", "  ")
	if err != nil {
		return body
	}
	return string(out)
}

// criticize is the scripted critic: it removes the last decision that no
// contradicted claim depends on, when there are at least two, and says so
// in the work log.
func criticize(body []byte) string {
	var br map[string]any
	if json.Unmarshal(body, &br) != nil {
		return string(body)
	}
	decs, _ := br["decisions"].([]any)
	if len(decs) < 2 {
		return string(body)
	}
	needed := map[string]bool{}
	claims, _ := br["claims"].([]any)
	for _, c := range claims {
		if m, ok := c.(map[string]any); ok && m["status"] == "contradicted" {
			if id, _ := m["decision"].(string); id != "" {
				needed[id] = true
			}
		}
	}
	drop := -1
	for i := len(decs) - 1; i >= 0; i-- {
		m, _ := decs[i].(map[string]any)
		if id, _ := m["id"].(string); m != nil && !needed[id] {
			drop = i
			break
		}
	}
	if drop < 0 {
		return string(body)
	}
	gone, _ := decs[drop].(map[string]any)
	id, _ := gone["id"].(string)
	title, _ := gone["title"].(string)
	br["decisions"] = append(decs[:drop:drop], decs[drop+1:]...)
	for _, c := range claims {
		if m, ok := c.(map[string]any); ok && m["decision"] == id {
			m["decision"] = ""
		}
	}
	if hits, ok := br["ruleHits"].([]any); ok {
		for _, h := range hits {
			if m, ok := h.(map[string]any); ok && m["decision"] == id {
				m["decision"] = ""
			}
		}
	}
	if since, ok := br["since"].(map[string]any); ok {
		if ids, ok := since["new"].([]any); ok {
			keep := []any{}
			for _, x := range ids {
				if x != id {
					keep = append(keep, x)
				}
			}
			since["new"] = keep
		}
	}
	work, _ := br["work"].([]any)
	br["work"] = append(work, map[string]any{"did": "Critic removed: " + title, "result": "The code one frame up already prevents this case; it is not a question for the reviewer."})
	out, err := json.MarshalIndent(br, "", "  ")
	if err != nil {
		return string(body)
	}
	return string(out)
}
