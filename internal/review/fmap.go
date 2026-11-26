package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/devenjarvis/docket/internal/config"
)

// Seen is how well the reviewer knows one part of one repository, judged
// only by what they have reviewed through Docket.
type Seen struct {
	Reviews int       `json:"reviews"`
	Last    time.Time `json:"last"`
}

// Map is the reviewer's map of every repository: repo → area → Seen. An area
// is a top-level directory, or the first two levels when the repository
// keeps everything under one directory such as src/ or internal/.
type Map map[string]map[string]Seen

func mapPath(home string) string { return filepath.Join(home, "map.json") }

func loadMap(home string) Map {
	m := Map{}
	if b, err := os.ReadFile(mapPath(home)); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

var deepRoots = map[string]bool{"src": true, "internal": true, "pkg": true, "lib": true, "app": true, "apps": true, "packages": true, "services": true, "cmd": true}

// Area names the part of a repository a file belongs to.
func Area(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) == 1 {
		return "(root)"
	}
	if deepRoots[parts[0]] && len(parts) > 2 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// Areas returns the distinct areas of paths, sorted.
func Areas(paths []string) []string {
	set := map[string]bool{}
	for _, p := range paths {
		set[Area(p)] = true
	}
	out := make([]string, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// touch records that the reviewer read these areas of repo today.
func touch(home, repo string, areas []string) {
	m := loadMap(home)
	if m[repo] == nil {
		m[repo] = map[string]Seen{}
	}
	for _, a := range areas {
		s := m[repo][a]
		s.Reviews++
		s.Last = time.Now()
		m[repo][a] = s
	}
	_ = config.WriteJSON(mapPath(home), m)
}
