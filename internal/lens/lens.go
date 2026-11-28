// Package lens is the panel of focused reviewers Docket can run before the
// main read: one short Markdown file per lens under ~/.docket/lenses, each
// saying what that reviewer looks for. Docket ships a set; the user can edit
// them, turn them off, reset them, and write their own.
//
// A lens file is front matter and a body:
//
//	---
//	name: Security
//	on: true
//	---
//	Look for ways this change lets someone do what they should not. …
//
// The body is the lens's focus. Docket wraps it in the lens prompt, which
// fixes where the files are and the shape of what the lens hands back, so a
// lens can say anything about what to look for without breaking the read.
package lens

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed defaults/*.md
var defaults embed.FS

// order is the order the built-in lenses are listed and run in.
var order = []string{"correctness", "claims", "tests", "security", "conventions", "compatibility", "performance"}

// MaxText bounds a lens's body, so one lens cannot crowd the prompt.
const MaxText = 4000

// Lens is one focused reviewer.
type Lens struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	On   bool   `json:"on"`
	Text string `json:"text"`
	// Builtin says Docket ships this lens, so it can be reset.
	Builtin bool `json:"builtin"`
	// Edited says a built-in lens's text differs from what Docket ships.
	Edited bool `json:"edited,omitempty"`
}

// Store is the lens directory.
type Store struct {
	dir string
	mu  sync.Mutex
}

// Open returns the lenses under home. The first time, it writes the
// built-in set; after that, what is on disk is the user's, and a built-in
// lens they deleted stays deleted until they restore it.
func Open(home string) (*Store, error) {
	dir := filepath.Join(home, "lenses")
	s := &Store{dir: dir}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		if err := s.Restore(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return s, nil
}

// Dir is where the lens files are.
func (s *Store) Dir() string { return s.dir }

// Defaults is the built-in set, as Docket ships it.
func Defaults() []Lens {
	out := make([]Lens, 0, len(order))
	for _, id := range order {
		b, err := defaults.ReadFile("defaults/" + id + ".md")
		if err != nil {
			panic(err) // embedded; a missing file is a build error
		}
		l, err := parse(id, string(b))
		if err != nil {
			panic(err)
		}
		l.Builtin = true
		out = append(out, l)
	}
	return out
}

func builtin(id string) (Lens, bool) {
	for _, l := range Defaults() {
		if l.ID == id {
			return l, true
		}
	}
	return Lens{}, false
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// ID turns a name into a lens id: lower case, words joined by hyphens.
func ID(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	id := strings.Trim(b.String(), "-")
	if len(id) > 40 {
		id = strings.Trim(id[:40], "-")
	}
	return id
}

func parse(id, s string) (Lens, error) {
	l := Lens{ID: id, On: true}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if rest, ok := strings.CutPrefix(s, "---\n"); ok {
		head, body, found := strings.Cut(rest, "\n---")
		if !found {
			return l, fmt.Errorf("lens %s: front matter has no closing ---", id)
		}
		for _, ln := range strings.Split(head, "\n") {
			k, v, ok := strings.Cut(ln, ":")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			switch strings.TrimSpace(k) {
			case "name":
				l.Name = v
			case "on":
				l.On = v != "false" && v != "no" && v != "off"
			}
		}
		s = strings.TrimPrefix(body, "\n")
	}
	l.Text = strings.TrimSpace(s)
	if l.Name == "" {
		l.Name = id
	}
	return l, nil
}

func format(l Lens) string {
	return fmt.Sprintf("---\nname: %s\non: %t\n---\n%s\n", strings.ReplaceAll(l.Name, "\n", " "), l.On, strings.TrimSpace(l.Text))
}

// List returns every lens: the built-in ones in their order, then the
// user's own by name. Files that do not parse are skipped.
func (s *Store) List() ([]Lens, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list()
}

func (s *Store) list() ([]Lens, error) {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Lens{}, nil
		}
		return nil, err
	}
	rank := map[string]int{}
	for i, id := range order {
		rank[id] = i + 1
	}
	out := []Lens{}
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || e.IsDir() || !idRe.MatchString(id) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		l, err := parse(id, string(b))
		if err != nil || l.Text == "" {
			continue
		}
		if d, ok := builtin(id); ok {
			l.Builtin = true
			l.Edited = d.Text != l.Text || d.Name != l.Name
		}
		out = append(out, l)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank[out[i].ID], rank[out[j].ID]
		switch {
		case ri > 0 && rj > 0:
			return ri < rj
		case ri > 0 || rj > 0:
			return ri > 0
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Active returns the lenses that are on, in the order they run.
func (s *Store) Active() ([]Lens, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var on []Lens
	for _, l := range all {
		if l.On {
			on = append(on, l)
		}
	}
	return on, nil
}

// Get returns one lens.
func (s *Store) Get(id string) (Lens, bool) {
	all, _ := s.List()
	for _, l := range all {
		if l.ID == id {
			return l, true
		}
	}
	return Lens{}, false
}

// Save writes a lens, creating it when its id is new. A lens with no id
// takes one from its name.
func (s *Store) Save(l Lens) (Lens, error) {
	l.Name = strings.TrimSpace(l.Name)
	l.Text = strings.TrimSpace(l.Text)
	if l.ID == "" {
		l.ID = ID(l.Name)
	}
	switch {
	case !idRe.MatchString(l.ID):
		return l, errors.New("give the lens a name with at least one letter or digit")
	case l.Name == "":
		return l, errors.New("give the lens a name")
	case l.Text == "":
		return l, errors.New("say what the lens looks for")
	case len(l.Text) > MaxText:
		return l, fmt.Errorf("keep a lens under %d characters; this one is %d", MaxText, len(l.Text))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return l, err
	}
	p := filepath.Join(s.dir, l.ID+".md")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(format(l)), 0o644); err != nil {
		return l, err
	}
	if err := os.Rename(tmp, p); err != nil {
		return l, err
	}
	if d, ok := builtin(l.ID); ok {
		l.Builtin, l.Edited = true, d.Text != l.Text || d.Name != l.Name
	}
	return l, nil
}

// Delete removes a lens.
func (s *Store) Delete(id string) error {
	if !idRe.MatchString(id) {
		return errors.New("no such lens")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(filepath.Join(s.dir, id+".md"))
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("no such lens")
	}
	return err
}

// Reset puts a built-in lens's name and text back to what Docket ships,
// keeping whether it is on.
func (s *Store) Reset(id string) (Lens, error) {
	d, ok := builtin(id)
	if !ok {
		return Lens{}, errors.New("only a lens Docket ships can be reset")
	}
	if cur, ok := s.Get(id); ok {
		d.On = cur.On
	}
	return s.Save(d)
}

// Restore writes every built-in lens that is missing, as Docket ships it.
// Lenses that are there, edited or not, are kept.
func (s *Store) Restore() error {
	have := map[string]bool{}
	all, err := s.List()
	if err != nil {
		return err
	}
	for _, l := range all {
		have[l.ID] = true
	}
	for _, d := range Defaults() {
		if have[d.ID] {
			continue
		}
		if _, err := s.Save(d); err != nil {
			return err
		}
	}
	return nil
}
