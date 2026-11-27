package tend

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"text/template"

	"github.com/devenjarvis/docket/internal/diff"
	"github.com/devenjarvis/docket/internal/model"
)

// MaxAutoLines is the most changed lines a fix may have and still run
// without asking.
const MaxAutoLines = 60

// GatePrefix starts every classifier prompt, so a scripted agent can tell it
// apart.
const GatePrefix = "You decide whether one step an agent proposes"

//go:embed gate.md
var gateText string

var gateTmpl = template.Must(template.New("gate").Parse(gateText))

var manifests = map[string]bool{
	"go.mod": true, "go.sum": true, "go.work": true, "go.work.sum": true,
	"package.json": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "bun.lockb": true, "npm-shrinkwrap.json": true,
	"cargo.toml": true, "cargo.lock": true,
	"pyproject.toml": true, "poetry.lock": true, "uv.lock": true, "pipfile": true, "pipfile.lock": true, "setup.py": true, "setup.cfg": true,
	"gemfile": true, "gemfile.lock": true, "composer.json": true, "composer.lock": true,
	"build.gradle": true, "build.gradle.kts": true, "pom.xml": true, "mix.exs": true, "mix.lock": true,
	"package.swift": true, "package.resolved": true, "podfile": true, "podfile.lock": true,
}

var toolchains = map[string]bool{
	".tool-versions": true, ".nvmrc": true, ".node-version": true, ".python-version": true, ".ruby-version": true,
	"rust-toolchain": true, "rust-toolchain.toml": true, ".go-version": true, ".java-version": true,
	"dockerfile": true, "flake.nix": true, "flake.lock": true,
}

var ciDirs = []string{".github/", ".circleci/", ".buildkite/", ".gitlab/", ".woodpecker/"}

var ciFiles = map[string]bool{".gitlab-ci.yml": true, "jenkinsfile": true, ".travis.yml": true, "azure-pipelines.yml": true, "bitbucket-pipelines.yml": true}

var requirementsRe = regexp.MustCompile(`^requirements.*\.(txt|in)$`)

var testFileRe = regexp.MustCompile(`(_test\.go|\.test\.[jt]sx?|\.spec\.[jt]sx?|_spec\.rb|^test_.*\.py|_test\.py|Test\.java|Tests?\.swift)$`)

var skipRe = regexp.MustCompile(`\bt\.Skip(Now|f)?\(|\b(it|test|describe)\.skip\(|\bx(it|describe)\(|@pytest\.mark\.skip|@unittest\.skip|@Disabled\b|@Ignore\b|\bskip\(\s*["']`)

// Floor applies the rules no classifier may overrule. It returns an ask
// verdict and true when one of them holds.
// MaxPatchChars is as much of a patch as the classifier is shown. A longer
// patch always waits for the user, so the classifier never judges a part.
const MaxPatchChars = 20000

func Floor(a model.TendAction, patch string) (model.Verdict, bool) {
	ask := func(reason string) (model.Verdict, bool) {
		return model.Verdict{Decision: model.GateAsk, Reason: reason, Floor: true}, true
	}
	switch a.Kind {
	case model.ActMergeBase:
		return ask("Resolving a conflict is a judgment call about two people's changes.")
	case model.ActReply:
		switch a.Stance {
		case "agrees", "informs":
		default:
			return ask("This reply " + stanceWords(a.Stance) + " a reviewer, in your name.")
		}
	case model.ActFix:
		files := diff.Parse(patch)
		if len(files) == 0 {
			return ask("Docket could not read this change, so it cannot judge it.")
		}
		if len(patch) > MaxPatchChars {
			return ask("The change is too long to judge in full.")
		}
		changed := 0
		for _, f := range files {
			if f.Path == "" {
				return ask("Docket could not read a file name in this change.")
			}
			if f.Binary {
				return ask(fmt.Sprintf("It changes a binary file (%s).", f.Path))
			}
			// A file moved away is judged as the file it was, too.
			for _, name := range []string{f.Path, f.OldPath} {
				if name == "" {
					continue
				}
				p := strings.ToLower(name)
				base := path.Base(p)
				switch {
				case manifests[base] || requirementsRe.MatchString(base):
					return ask(fmt.Sprintf("It changes dependencies (%s).", name))
				case toolchains[base]:
					return ask(fmt.Sprintf("It changes the toolchain (%s).", name))
				case ciFiles[base] || hasPrefix(p, ciDirs):
					return ask(fmt.Sprintf("It changes CI configuration (%s).", name))
				}
			}
			if f.OldPath != "" && testFileRe.MatchString(path.Base(f.OldPath)) && !testFileRe.MatchString(path.Base(f.Path)) {
				return ask(fmt.Sprintf("It moves a test file out of the tests (%s).", f.OldPath))
			}
			if testFileRe.MatchString(path.Base(f.Path)) {
				if f.Status == "deleted" {
					return ask(fmt.Sprintf("It deletes a test file (%s).", f.Path))
				}
				for _, h := range f.Hunks {
					for _, l := range h.Lines {
						if l.Kind == "+" && skipRe.MatchString(l.Text) {
							return ask(fmt.Sprintf("It skips a test (%s).", f.Path))
						}
						if l.Kind == "-" && strings.Contains(l.Text, "func Test") {
							return ask(fmt.Sprintf("It removes a test (%s).", f.Path))
						}
					}
				}
			}
			changed += f.Additions + f.Deletions
		}
		if changed > MaxAutoLines {
			return ask(fmt.Sprintf("It changes %d lines; more than %d always waits for you.", changed, MaxAutoLines))
		}
	}
	return model.Verdict{}, false
}

func stanceWords(s string) string {
	switch s {
	case "declines":
		return "declines what was asked of"
	case "asks":
		return "asks a question back to"
	}
	return "answers"
}

func hasPrefix(p string, prefixes []string) bool {
	for _, pre := range prefixes {
		if strings.HasPrefix(p, pre) || strings.Contains(p, "/"+pre) {
			return true
		}
	}
	return false
}

// GateData fills the classifier prompt.
type GateData struct {
	Action  model.TendAction
	Patch   string
	Context string
}

// GatePrompt is the classifier's instruction for one action.
func GatePrompt(a model.TendAction, patch, context string) string {
	var b strings.Builder
	if len(patch) > MaxPatchChars {
		patch = patch[:MaxPatchChars] + "\n… (cut)"
	}
	if err := gateTmpl.Execute(&b, GateData{Action: a, Patch: patch, Context: context}); err != nil {
		panic(err)
	}
	return b.String()
}

var objectRe = regexp.MustCompile(`\{[^{}]*"decision"[^{}]*\}`)

// Classify asks a model whether an action may run without the author. run
// sends a prompt and returns the reply. Anything but a clear auto is ask:
// a classifier that fails or rambles must not be what lets a change through.
func Classify(ctx context.Context, run func(context.Context, string) (string, error), a model.TendAction, patch, context string) model.Verdict {
	out, err := run(ctx, GatePrompt(a, patch, context))
	if err != nil {
		return model.Verdict{Decision: model.GateAsk, Reason: "The classifier could not run: " + err.Error()}
	}
	matches := objectRe.FindAllString(out, -1)
	if len(matches) == 0 {
		return model.Verdict{Decision: model.GateAsk, Reason: "The classifier gave no verdict."}
	}
	var v struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if json.Unmarshal([]byte(matches[len(matches)-1]), &v) != nil {
		return model.Verdict{Decision: model.GateAsk, Reason: "The classifier gave no verdict."}
	}
	reason := strings.TrimSpace(v.Reason)
	switch v.Decision {
	case model.GateAuto:
		if reason == "" {
			reason = "Small and clearly asked for."
		}
		return model.Verdict{Decision: model.GateAuto, Reason: reason}
	case model.GateAsk:
		if reason == "" {
			reason = "The classifier wants you to look."
		}
		return model.Verdict{Decision: model.GateAsk, Reason: reason}
	}
	return model.Verdict{Decision: model.GateAsk, Reason: "The classifier gave no verdict."}
}
