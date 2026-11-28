package desktop

import (
	"os"
	"regexp"
	"testing"
)

// macOS posts notifications only for an app with a bundle identifier, and
// the single-instance lock uses BundleID too, so the bundle's Info.plist
// must name the same one.
func TestInfoPlistBundleID(t *testing.T) {
	b, err := os.ReadFile("../../build/darwin/Info.plist")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`<key>CFBundleIdentifier</key>\s*<string>([^<]*)</string>`).FindSubmatch(b)
	if m == nil {
		t.Fatal("Info.plist has no CFBundleIdentifier")
	}
	if got := string(m[1]); got != BundleID {
		t.Errorf("Info.plist CFBundleIdentifier = %q, want BundleID %q", got, BundleID)
	}
}
