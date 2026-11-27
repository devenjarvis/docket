package tend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/devenjarvis/docket/internal/model"
)

func patch(path string, add, del int, extra string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n@@ -1,%d +1,%d @@\n", path, path, path, path, del, add)
	for i := 0; i < del; i++ {
		fmt.Fprintf(&b, "-old %d\n", i)
	}
	for i := 0; i < add; i++ {
		fmt.Fprintf(&b, "+new %d\n", i)
	}
	b.WriteString(extra)
	return b.String()
}

func TestFloor(t *testing.T) {
	fix := model.TendAction{Kind: model.ActFix}
	deleted := "diff --git a/x_test.go b/x_test.go\ndeleted file mode 100644\n--- a/x_test.go\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-package x\n-func TestX() {}\n"
	cases := []struct {
		name  string
		a     model.TendAction
		patch string
		ask   bool
	}{
		{"small rename", fix, patch("src/cache.go", 2, 2, ""), false},
		{"go.mod", fix, patch("go.mod", 1, 1, ""), true},
		{"lockfile", fix, patch("web/package-lock.json", 3, 3, ""), true},
		{"ci config", fix, patch(".github/workflows/ci.yml", 1, 1, ""), true},
		{"toolchain", fix, patch(".tool-versions", 1, 1, ""), true},
		{"deleted test", fix, deleted, true},
		{"skipped test", fix, "diff --git a/x_test.go b/x_test.go\n--- a/x_test.go\n+++ b/x_test.go\n@@ -1,1 +1,2 @@\n func TestX(t *testing.T) {\n+\tt.Skip(\"flaky\")\n", true},
		{"61 changed lines", fix, patch("src/a.go", 31, 30, ""), true},
		{"60 changed lines", fix, patch("src/a.go", 30, 30, ""), false},
		{"conflict resolution", model.TendAction{Kind: model.ActMergeBase}, patch("src/a.go", 1, 1, ""), true},
		{"declining reply", model.TendAction{Kind: model.ActReply, Stance: "declines"}, "", true},
		{"agreeing reply", model.TendAction{Kind: model.ActReply, Stance: "agrees"}, "", false},
		{"rerun", model.TendAction{Kind: model.ActRerun}, "", false},
		{"no patch", fix, "", true},
		{"binary file", fix, "diff --git a/logo.png b/logo.png\nindex 1..2 100644\nBinary files a/logo.png and b/logo.png differ\n", true},
		{"long lines", fix, patch("src/a.go", 1, 1, "+"+strings.Repeat("x", MaxPatchChars)+"\n"), true},
		{"manifest in a folder with a space", fix, "diff --git a/my app/package.json b/my app/package.json\n--- a/my app/package.json\t\n+++ b/my app/package.json\t\n@@ -1,1 +1,1 @@\n-a\n+b\n", true},
		{"ci file moved away", fix, "diff --git a/.github/workflows/ci.yml b/docs/ci.txt\nsimilarity index 100%\nrename from .github/workflows/ci.yml\nrename to docs/ci.txt\n", true},
		{"go.mod moved away", fix, "diff --git a/go.mod b/x\nsimilarity index 100%\nrename from go.mod\nrename to x\n", true},
		{"test moved away", fix, "diff --git a/foo_test.go b/foo.txt\nsimilarity index 100%\nrename from foo_test.go\nrename to foo.txt\n", true},
		{"quoted path", fix, "diff --git \"a/\\303\\251/package.json\" \"b/\\303\\251/package.json\"\n--- \"a/\\303\\251/package.json\"\n+++ \"b/\\303\\251/package.json\"\n@@ -1,1 +1,1 @@\n-a\n+b\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, floored := Floor(c.a, c.patch)
			if floored != c.ask {
				t.Fatalf("floored = %v (%+v), want %v", floored, v, c.ask)
			}
			if floored && (v.Decision != model.GateAsk || !v.Floor || v.Reason == "") {
				t.Errorf("a floor must ask with a reason: %+v", v)
			}
		})
	}
}

func TestClassify(t *testing.T) {
	a := model.TendAction{Kind: model.ActFix, Why: "rename"}
	run := func(out string, err error) func(context.Context, string) (string, error) {
		return func(_ context.Context, prompt string) (string, error) {
			if !strings.Contains(prompt, "src/cache.go") {
				t.Error("the classifier was not shown the real patch")
			}
			return out, err
		}
	}
	p := patch("src/cache.go", 1, 1, "")
	cases := []struct {
		name, out string
		err       error
		want      string
	}{
		{"auto", `I looked. {"decision":"auto","reason":"a rename the reviewer asked for"}`, nil, model.GateAuto},
		{"ask", `{"decision":"ask","reason":"changes behaviour"}`, nil, model.GateAsk},
		{"garbage", `sure, go ahead`, nil, model.GateAsk},
		{"odd decision", `{"decision":"yes"}`, nil, model.GateAsk},
		{"error", "", errors.New("agent died"), model.GateAsk},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := Classify(context.Background(), run(c.out, c.err), a, p, "thread text")
			if v.Decision != c.want || v.Reason == "" {
				t.Errorf("verdict = %+v, want %s with a reason", v, c.want)
			}
		})
	}
}
