package web

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// docs/tokens.json is the design system's record of Whiteprint, and the
// Design System artifact is built from it. docket.css is what the app runs.
// This test fails when they disagree, so neither drifts from the other.

type tokenFile struct {
	Color struct {
		Themes []struct{ ID string } `json:"themes"`
		Tokens []struct {
			Name  string            `json:"name"`
			Value map[string]string `json:"value"`
		} `json:"tokens"`
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
}

var cssVar = regexp.MustCompile(`--([\w-]+):\s*([^;]+);`)

// cssBlock returns the custom properties declared in the first rule whose
// selector is sel.
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

func TestTokensMatchStylesheet(t *testing.T) {
	raw, err := os.ReadFile("../../docs/tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	var tf tokenFile
	if err := json.Unmarshal(raw, &tf); err != nil {
		t.Fatalf("docs/tokens.json: %v", err)
	}
	cssRaw, err := files.ReadFile("static/docket.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssRaw)
	themes := map[string]map[string]string{
		"whiteprint": cssBlock(t, css, ":root"),
		"blueprint":  cssBlock(t, css, `[data-theme="blueprint"]`),
	}
	if len(tf.Color.Themes) != 2 || tf.Color.Themes[0].ID != "whiteprint" || tf.Color.Themes[1].ID != "blueprint" {
		t.Fatalf("tokens.json themes must be whiteprint then blueprint, got %+v", tf.Color.Themes)
	}

	// Every colour token is declared in both themes with the same value.
	named := map[string]bool{}
	for _, tok := range tf.Color.Tokens {
		named[tok.Name] = true
		for theme, vars := range themes {
			got, ok := vars[tok.Name]
			if !ok {
				t.Errorf("--%s is in tokens.json but not declared for %s in docket.css", tok.Name, theme)
				continue
			}
			if normColor(got) != normColor(tok.Value[theme]) {
				t.Errorf("--%s (%s): docket.css has %s, tokens.json has %s", tok.Name, theme, got, tok.Value[theme])
			}
		}
	}
	// Every colour docket.css declares in the dark theme is a token: a new
	// colour has to be recorded before it ships.
	for name := range themes["blueprint"] {
		if !named[name] {
			t.Errorf("docket.css declares --%s for blueprint, but tokens.json does not record it", name)
		}
	}

	root := themes["whiteprint"]
	find := func(list []struct{ Name, Value string }, name string) string {
		for _, x := range list {
			if x.Name == name {
				return x.Value
			}
		}
		return ""
	}
	for css, tok := range map[string]string{"u": find(tf.Spacing.Tokens, "u"), "line": find(tf.Stroke.Tokens, "line"), "heavy": find(tf.Stroke.Tokens, "heavy")} {
		if root[css] != tok {
			t.Errorf("--%s: docket.css has %q, tokens.json has %q", css, root[css], tok)
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
