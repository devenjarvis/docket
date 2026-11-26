package model

import (
	"fmt"
	"regexp"
	"strconv"
)

// Key formats a PR's stable id.
func Key(owner, repo string, number int) string {
	return fmt.Sprintf("%s/%s#%d", owner, repo, number)
}

var refRe = regexp.MustCompile(`^(?:https?://)?(?:github\.com/)?([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)(?:/pull/|#)(\d+)`)

// ParseRef reads "owner/repo#12", "github.com/owner/repo/pull/12" or the full
// URL, with anything after the number ignored.
func ParseRef(s string) (owner, repo string, number int, ok bool) {
	m := refRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", 0, false
	}
	n, err := strconv.Atoi(m[3])
	if err != nil || n <= 0 {
		return "", "", 0, false
	}
	return m[1], m[2], n, true
}
