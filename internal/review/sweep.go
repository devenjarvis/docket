package review

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/model"
	"github.com/devenjarvis/docket/internal/repo"
)

// sweep removes the checkouts under reviews/ that no review needs any more:
// every head of a pull request that has left the queue, and every head of a
// pull request still in the queue other than the head it was read at and the
// head it is at now. One being read, or waiting to be read, keeps all of its
// heads.
//
// It runs after every sync, so checkouts left by earlier versions of Docket
// go too.
func (e *Engine) sweep(ctx context.Context) {
	root := filepath.Join(e.Home, "reviews")
	type stale struct {
		key string
		pr  model.PR
	}
	var gone []stale
	ours := map[string]bool{}
	e.mu.Lock()
	for _, d := range dirs(root, 4) {
		rel, err := filepath.Rel(root, d)
		if err != nil {
			continue
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 4 {
			continue
		}
		owner, name, sha := parts[0], parts[1], parts[3]
		n, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}
		key := model.Key(owner, name, n)
		if (e.sweeping[key] && !ours[key]) || !e.stale(key, sha) {
			continue
		}
		e.sweeping[key], ours[key] = true, true
		gone = append(gone, stale{key, model.PR{Owner: owner, Repo: name, Number: n, HeadSHA: sha}})
	}
	e.mu.Unlock()

	for _, g := range gone {
		ws := repo.Layout(e.Home, g.pr)
		e.Prep.Release(ctx, g.pr, ws)
		if err := os.RemoveAll(ws.Dir); err != nil {
			e.Logf("could not remove %s: %v", ws.Dir, err)
		}
		// Drop the pull request's and repository's directories once empty.
		for p := filepath.Dir(ws.Dir); p != root; p = filepath.Dir(p) {
			if os.Remove(p) != nil {
				break
			}
		}
	}
	e.mu.Lock()
	for key := range ours {
		delete(e.sweeping, key)
	}
	e.mu.Unlock()
}

// stale reports whether the checkout of key at sha can go. e.mu is held.
func (e *Engine) stale(key, sha string) bool {
	if e.cancels[key] != nil || e.queued[key] {
		return false
	}
	r := e.reviews[key]
	if r == nil {
		return true
	}
	if r.Status == "queued" || r.Status == "reading" {
		return false
	}
	return sha != dirSHA(r.ReadSHA) && sha != dirSHA(r.PR.HeadSHA)
}

// dirSHA is a head as repo.Layout names its directory.
func dirSHA(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

// dirs lists the directories exactly depth levels below root.
func dirs(root string, depth int) []string {
	level := []string{root}
	for i := 0; i < depth; i++ {
		var next []string
		for _, d := range level {
			entries, err := os.ReadDir(d)
			if err != nil {
				continue
			}
			for _, en := range entries {
				if en.IsDir() {
					next = append(next, filepath.Join(d, en.Name()))
				}
			}
		}
		level = next
	}
	return level
}

// waitSweep blocks, with e.mu held, until no sweep is removing key's
// checkouts, so a read never prepares a checkout a sweep is deleting.
func (e *Engine) waitSweep(key string) {
	for e.sweeping[key] {
		e.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		e.mu.Lock()
	}
}
