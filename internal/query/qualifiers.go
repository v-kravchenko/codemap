package query

// Resource qualifiers of a change: a resource changed only in values-night/ needs only the night
// render of the screens it reaches.

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/v-kravchenko/codemap/internal/android"
	"github.com/v-kravchenko/codemap/internal/util"
)

// resChange: a changed file defining a resource changes names, at the resource's line (0: none,
// a file resource or a value the head removed), with the qualifiers of its directory ("": none).
type resChange struct {
	path      string
	line      int
	qualifier string
}

// resQualifier: the qualifiers of a resource file's directory: night-v31 of values-night-v31/.
func resQualifier(path string) string {
	_, q, _ := strings.Cut(filepath.Base(filepath.Dir(path)), "-")
	return q
}

// noteResChanges: the changed file of each resource seed of path, for its location and qualifiers.
func (g *Graph) noteResChanges(path, text string, seeds []Seed) {
	lines := map[string]int{}
	if android.ResourceTypeOfDir(filepath.Base(filepath.Dir(path))) == "values" {
		for i, line := range strings.Split(text, "\n") {
			for _, m := range valueStart.FindAllStringSubmatch(line, -1) {
				rtype := android.ValueTags[m[1]]
				if rtype == "" && m[1] == "item" {
					rtype = m[3]
				}
				if k := android.ResKey(rtype, m[2]); rtype != "" && lines[k] == 0 {
					lines[k] = i + 1
				}
			}
		}
	}
	for _, s := range seeds {
		if g.kind(s.N) == "R" {
			g.resChanges[s.N] = append(g.resChanges[s.N], resChange{path, lines[g.name(s.N)], resQualifier(path)})
		}
	}
}

// changedLocation: where the change of a resource is, the first of its changed files, unqualified
// first (values-night/ sorts before values/); else the symbol's own location.
func (g *Graph) changedLocation(n int32) string {
	rc := g.resChanges[n]
	if len(rc) == 0 {
		return g.location(n)
	}
	first := slices.MinFunc(rc, func(a, b resChange) int {
		if (a.qualifier == "") != (b.qualifier == "") {
			if a.qualifier == "" {
				return -1
			}
			return 1
		}
		return strings.Compare(a.path, b.path)
	})
	if first.line > 0 {
		return fmt.Sprintf("%s:%d", first.path, first.line)
	}
	return first.path
}

// qualifiersOf: the qualifiers of a changed resource's files, sorted; empty when a file without
// any changed it (every configuration); ok false for code.
func (g *Graph) qualifiersOf(n int32) ([]string, bool) {
	rc, ok := g.resChanges[n]
	if !ok {
		return nil, false
	}
	out := []string{}
	for _, c := range rc {
		if c.qualifier == "" {
			return []string{}, true
		}
		out = append(out, c.qualifier)
	}
	slices.Sort(out)
	return slices.Compact(out), true
}

// screenQualifiers: for each screen reached, the qualifiers of the changes reaching it when only
// qualified resources do (a night-only change: night); nil, every configuration, when code or an
// unqualified resource reaches it. A walk of those, then one per qualifier.
func (g *Graph) screenQualifiers(seeds []Seed, depth int) map[int32][]string {
	var plain []Seed
	byQualifier := map[string][]Seed{}
	for _, s := range seeds {
		if qs, ok := g.qualifiersOf(s.N); ok && len(qs) > 0 {
			for _, q := range qs {
				byQualifier[q] = append(byQualifier[q], s)
			}
		} else {
			plain = append(plain, s)
		}
	}
	out := map[int32][]string{}
	if len(byQualifier) == 0 {
		return out
	}
	every := map[int32]bool{}
	if len(plain) > 0 {
		for n := range g.impact(plain, depth).Screens {
			every[n] = true
		}
	}
	for _, q := range util.SortedKeys(byQualifier) {
		for n := range g.impact(byQualifier[q], depth).Screens {
			if !every[n] {
				out[n] = append(out[n], q)
			}
		}
	}
	return out
}
