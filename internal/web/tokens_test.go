package web

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// docs/tokens.json is the design system's record of Overprint, and the
// Design System artifact is built from it. docket.css is what the app runs.
// These tests fail when they disagree, so neither drifts from the other, and
// when a colourway lets a pair a component relies on drop under its contrast.

type tokenFile struct {
	Color struct {
		Modes   []struct{ ID string } `json:"modes"`
		Default string                `json:"default"`
		Tokens  []struct {
			Name string `json:"name"`
		} `json:"tokens"`
		Colourways []struct {
			ID    string            `json:"id"`
			Name  string            `json:"name"`
			Light map[string]string `json:"light"`
			Dark  map[string]string `json:"dark"`
		} `json:"colourways"`
		Pairs []struct {
			Fg  string  `json:"fg"`
			Bg  string  `json:"bg"`
			Min float64 `json:"min"`
		} `json:"pairs"`
	} `json:"color"`
	Type struct {
		Fonts []struct {
			Family string `json:"family"`
			File   string `json:"file"`
		} `json:"fonts"`
		Families map[string]string `json:"families"`
	} `json:"type"`
	Spacing struct {
		Tokens []struct{ Name, Value string } `json:"tokens"`
	} `json:"spacing"`
	Stroke struct {
		Tokens []struct{ Name, Value string } `json:"tokens"`
	} `json:"stroke"`
	Offset struct {
		Tokens []struct{ Name, Value string } `json:"tokens"`
	} `json:"offset"`
}

var cssVar = regexp.MustCompile(`--([\w-]+):\s*([^;]+);`)

// cssBlock returns the custom properties declared in the first rule whose
// selector list ends with sel.
func cssBlock(t *testing.T, css, sel string) map[string]string {
	t.Helper()
	i := strings.Index(css, sel+" {")
	if i < 0 {
		t.Fatalf("docket.css has no %s block", sel)
	}
	end := strings.Index(css[i:], "\n}")
	out := map[string]string{}
	for _, m := range cssVar.FindAllStringSubmatch(css[i:i+end], -1) {
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

func normColor(v string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), " ", ""))
}

func readTokens(t *testing.T) (tokenFile, string) {
	t.Helper()
	raw, err := os.ReadFile("../../docs/tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	var tf tokenFile
	if err := json.Unmarshal(raw, &tf); err != nil {
		t.Fatalf("docs/tokens.json: %v", err)
	}
	css, err := files.ReadFile("static/docket.css")
	if err != nil {
		t.Fatal(err)
	}
	return tf, string(css)
}

func modes(tf tokenFile, i int) map[string]map[string]string {
	c := tf.Color.Colourways[i]
	return map[string]map[string]string{"light": c.Light, "dark": c.Dark}
}

func TestTokensMatchStylesheet(t *testing.T) {
	tf, css := readTokens(t)
	if len(tf.Color.Modes) != 2 || tf.Color.Modes[0].ID != "light" || tf.Color.Modes[1].ID != "dark" {
		t.Fatalf("tokens.json modes must be light then dark, got %+v", tf.Color.Modes)
	}
	if len(tf.Color.Colourways) == 0 || tf.Color.Colourways[0].ID != tf.Color.Default {
		t.Fatalf("the default colourway %q must be listed first", tf.Color.Default)
	}
	named := map[string]bool{}
	for _, tok := range tf.Color.Tokens {
		named[tok.Name] = true
	}

	// Every colourway is declared in both modes with exactly the recorded
	// tokens, and the default light block is also :root.
	for i, cw := range tf.Color.Colourways {
		for mode, want := range modes(tf, i) {
			sel := fmt.Sprintf(`[data-ink="%s"][data-theme="%s"]`, cw.ID, mode)
			got := cssBlock(t, css, sel)
			for _, tok := range tf.Color.Tokens {
				w, ok := want[tok.Name]
				if !ok {
					t.Errorf("%s %s: tokens.json gives no value for --%s", cw.ID, mode, tok.Name)
					continue
				}
				if normColor(got[tok.Name]) != normColor(w) {
					t.Errorf("--%s (%s %s): docket.css has %q, tokens.json has %q", tok.Name, cw.ID, mode, got[tok.Name], w)
				}
			}
			for name := range got {
				if !named[name] {
					t.Errorf("docket.css declares --%s for %s %s, but tokens.json does not record it", name, cw.ID, mode)
				}
			}
		}
	}
	if !strings.Contains(css, fmt.Sprintf(`:root, [data-ink="%s"][data-theme="light"] {`, tf.Color.Default)) {
		t.Errorf("the default colourway's light block must also be :root, so a page with no data-ink still has colour")
	}
	// The colourways the settings offer and the ones set before first paint
	// are the ones the stylesheet has.
	for _, f := range []string{"static/settings.js", "static/index.html", "static/styleguide.html"} {
		raw, err := files.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, cw := range tf.Color.Colourways {
			if !strings.Contains(string(raw), `"`+cw.ID+`"`) {
				t.Errorf("%s does not offer the colourway %s", f, cw.ID)
			}
		}
	}

	root := cssBlock(t, css, ":root")
	find := func(list []struct{ Name, Value string }, name string) string {
		for _, x := range list {
			if x.Name == name {
				return x.Value
			}
		}
		return ""
	}
	for v, tok := range map[string]string{
		"u": find(tf.Spacing.Tokens, "u"), "line": find(tf.Stroke.Tokens, "line"),
		"heavy": find(tf.Stroke.Tokens, "heavy"), "off": find(tf.Offset.Tokens, "off"),
	} {
		if root[v] != tok {
			t.Errorf("--%s: docket.css has %q, tokens.json has %q", v, root[v], tok)
		}
	}
	for key, stack := range tf.Type.Families {
		if got := root[key]; got != stack {
			t.Errorf("--%s: docket.css has %q, tokens.json has %q", key, got, stack)
		}
	}

	// Every font the tokens name is bundled, and declared with @font-face.
	for _, f := range tf.Type.Fonts {
		if _, err := files.ReadFile("static/" + f.File); err != nil {
			t.Errorf("font %s: %v", f.File, err)
		}
		if !strings.Contains(css, `url("`+f.File+`")`) {
			t.Errorf("font %s has no @font-face in docket.css", f.File)
		}
	}
}

// TestContrast checks every pair a component relies on, in every colourway
// and both modes, against the minimum tokens.json records for it.
func TestContrast(t *testing.T) {
	tf, _ := readTokens(t)
	if len(tf.Color.Pairs) == 0 {
		t.Fatal("tokens.json records no contrast pairs")
	}
	for i, cw := range tf.Color.Colourways {
		for mode, v := range modes(tf, i) {
			for _, p := range tf.Color.Pairs {
				got := contrast(t, v[p.Fg], v[p.Bg])
				if got+1e-9 < p.Min {
					t.Errorf("%s %s: --%s on --%s is %.2f:1, needs %.1f:1", cw.ID, mode, p.Fg, p.Bg, got, p.Min)
				}
			}
		}
	}
}

func luminance(t *testing.T, hex string) float64 {
	t.Helper()
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		t.Fatalf("%q is not a #rrggbb colour", hex)
	}
	var c [3]float64
	for i := range c {
		n, err := strconv.ParseUint(hex[i*2:i*2+2], 16, 8)
		if err != nil {
			t.Fatalf("%q: %v", hex, err)
		}
		x := float64(n) / 255
		if x <= 0.03928 {
			c[i] = x / 12.92
		} else {
			c[i] = math.Pow((x+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}

func contrast(t *testing.T, a, b string) float64 {
	la, lb := luminance(t, a), luminance(t, b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
