package demo

import (
	"testing"

	"github.com/devenjarvis/docket/internal/brief"
	"github.com/devenjarvis/docket/internal/diff"
)

// Every brief the scripted agent can write passes the validator Docket runs
// on a real agent's, against its own diff.
func TestDemoBriefsHoldUp(t *testing.T) {
	rules := map[string]bool{"R03": true, "R07": true, "R09": true, "R12": true, "R15": true}
	for _, n := range []string{"4127", "311", "2214", "4133", "318", "2230", "4140", "4140-fixed"} {
		t.Run(n, func(t *testing.T) {
			b, err := brief.Parse([]byte(fixture(n + ".json")))
			if err != nil {
				t.Fatal(err)
			}
			files := diff.Parse(fixture(n + ".patch"))
			if len(files) == 0 {
				t.Fatal("no diff")
			}
			if probs := brief.Validate(b, files, rules); len(probs) > 0 {
				t.Errorf("problems: %q", probs)
			}
		})
	}
}
