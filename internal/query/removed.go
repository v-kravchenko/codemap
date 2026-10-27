package query

// Screens a change removes: the activities, fragments, dialogs and previews the base's text of a
// deleted or changed source declares and the map's revision has not. Read from the text, as added
// ones are (docs/decisions/0021): the map has only the head, and a base map would need the base
// compiled. Given one (--base-db), the screens of the base's map the head's has not
// (docs/decisions/0022).

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/v-kravchenko/codemap/internal/config"
)

type removedScreen struct{ kind, name, target, module, location string }

var (
	sourcePackage = regexp.MustCompile(`(?m)^\s*package\s+([\w.]+)`)
	sourceImport  = regexp.MustCompile(`(?m)^\s*import\s+(?:static\s+)?([\w.]+)\s*;?\s*$`)
	jvmName       = regexp.MustCompile(`@file:JvmName\("(\w+)"\)`)
	classHead     = regexp.MustCompile(`^((?:\w+\s+)*)class\s+(\w+)`)
	funHead       = regexp.MustCompile(`^(?:@[\w.]+(?:\([^)]*\))?\s+)*(?:\w+\s+)*fun\s+(?:<[^>]*>\s*)?(\w+)\s*\(`)
	otherHead     = regexp.MustCompile(`^(?:\w+\s+)*(?:class|object|interface|val|var|typealias)\b`)
	annotationUse = regexp.MustCompile(`@([\w.]+)`)
	javaExtends   = regexp.MustCompile(`\bextends\s+([\w.]+)`)
)

// declaredScreens: the screens a source's text declares at its top level: classes whose
// supertypes reach the configured bases, and functions with a preview annotation (Kotlin). Nested
// declarations are left out, as the map's screens leave out nested classes.
func (g *Graph) declaredScreens(path, text string) []removedScreen {
	pkg := ""
	if m := sourcePackage.FindStringSubmatch(text); m != nil {
		pkg = m[1] + "."
	}
	imports := map[string]string{}
	for _, m := range sourceImport.FindAllStringSubmatch(text, -1) {
		imports[m[1][strings.LastIndex(m[1], ".")+1:]] = m[1]
	}
	java := strings.HasSuffix(path, ".java")
	facade := capitalized(strings.TrimSuffix(filepath.Base(path), ".kt")) + "Kt"
	if m := jvmName.FindStringSubmatch(text); m != nil {
		facade = m[1]
	}
	lines := splitLines(text)
	var out []removedScreen
	preview := false
	for i, line := range lines {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		if m := classHead.FindStringSubmatch(line); m != nil {
			preview = false
			if strings.Contains(m[1], "abstract") || strings.Contains(m[1], "sealed") {
				continue
			}
			header := strings.Join(lines[i:min(i+30, len(lines))], "\n")[len(m[0]):]
			if kind := g.baseKind(superOf(header, java), pkg, imports); kind != "" {
				out = append(out, removedScreen{kind, m[2], pkg + m[2], "", path + ":" + strconv.Itoa(i+1)})
			}
			continue
		}
		for _, a := range annotationUse.FindAllStringSubmatch(line, -1) {
			if config.PreviewMarkers[a[1][strings.LastIndex(a[1], ".")+1:]] {
				preview = true
			}
		}
		if m := funHead.FindStringSubmatch(line); m != nil {
			if preview && !java {
				out = append(out, removedScreen{"preview", strings.TrimSuffix(facade, "Kt") + "." + m[1],
					pkg + facade + "." + m[1], "", path + ":" + strconv.Itoa(i+1)})
			}
			preview = false
		} else if otherHead.MatchString(line) {
			preview = false
		}
	}
	return out
}

// superOf: the first supertype of a class header (the text after its name), without type
// arguments: `(val x: Int) : Base<A>(x), I {` gives Base.
func superOf(header string, java bool) string {
	depth := 0
scan:
	for i, c := range header {
		switch {
		case c == '(' || c == '<':
			depth++
		case c == ')' || c == '>':
			depth--
		case depth == 0 && c == '{':
			header = header[:i]
			break scan
		case depth == 0 && c == ':' && !java:
			rest := strings.TrimSpace(header[i+1:])
			end := strings.IndexFunc(rest, func(r rune) bool { return r != '.' && r != '_' && !isWordRune(r) })
			if end < 0 {
				end = len(rest)
			}
			return rest[:end]
		}
	}
	if m := javaExtends.FindStringSubmatch(header); java && m != nil {
		return m[1]
	}
	return ""
}

func isWordRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

// baseKind: the screen kind a class extending sup is, by sup's names among the imports and the
// package, its supertypes in the map and the configured bases; "" when none is reached.
func (g *Graph) baseKind(sup, pkg string, imports map[string]string) string {
	if sup == "" {
		return ""
	}
	start := []string{sup}
	if !strings.Contains(sup, ".") {
		start = []string{pkg + sup}
		if fq, ok := imports[sup]; ok {
			start = []string{fq}
		}
	}
	hit, seen := map[string]bool{}, map[string]bool{}
	for len(start) > 0 {
		x := strings.ReplaceAll(start[len(start)-1], ".", "/")
		start = start[:len(start)-1]
		if seen[x] {
			continue
		}
		seen[x] = true
		hit["activity"] = hit["activity"] || config.ActivityBases[x]
		hit["dialog"] = hit["dialog"] || config.DialogBases[x]
		hit["fragment"] = hit["fragment"] || config.FragmentBases[x]
		if c, ok := g.classByName[x]; ok {
			for _, r := range g.supNames(c) {
				start = append(start, g.m.Hierarchy.Sup.At(int(r)))
			}
		}
	}
	for _, k := range []string{"activity", "dialog", "fragment"} {
		if hit[k] {
			return k
		}
	}
	return ""
}

// removedScreens: the screens the base's text of the changed sources declares that neither the
// map nor the head's text of the same file has; old gives the base's text of a path and the
// head's (ok false: unknown).
func (g *Graph) removedScreens(paths []string, old func(path string) (base, oldPath, head string, ok bool)) []removedScreen {
	targets := map[string]bool{}
	for _, rows := range g.screens {
		for _, r := range rows {
			targets[r.Target] = true
		}
	}
	var out []removedScreen
	for _, path := range paths {
		base, oldPath, head, ok := old(path)
		if !ok {
			continue
		}
		module := "?"
		if d, ok := g.moduleDir(oldPath); ok {
			for i := range g.m.Module.Dir.Len() {
				if g.m.Module.Dir.At(i) == d {
					module = g.m.Module.Name.At(i)
				}
			}
		}
		for _, s := range g.declaredScreens(oldPath, base) {
			name := s.target[strings.LastIndex(s.target, ".")+1:]
			if targets[s.target] || head != "" && declared(head, []string{name}, s.kind != "preview", strings.HasSuffix(path, ".java")) {
				continue
			}
			s.module = module
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b removedScreen) int {
		return slices.Compare([]string{a.kind, a.module, a.name, a.target, a.location},
			[]string{b.kind, b.module, b.name, b.target, b.location})
	})
	return slices.CompactFunc(out, func(a, b removedScreen) bool { return a == b })
}

// removedFromBase: the screens of the base's map that are not screens in the map's revision, with
// their location in the base.
func (g *Graph) removedFromBase() []removedScreen {
	out := []removedScreen{}
	b := g.base
	for _, n := range b.screenOrder {
		if h := g.m.Index(b.m.Node.ID[n]); h >= 0 && len(g.screens[h]) > 0 {
			continue
		}
		r := b.screens[n][0]
		out = append(out, removedScreen{r.Kind, r.Label, r.Target, b.module(n), b.location(n)})
	}
	slices.SortFunc(out, func(a, b removedScreen) int {
		return slices.Compare([]string{a.kind, a.module, a.name, a.target, a.location},
			[]string{b.kind, b.module, b.name, b.target, b.location})
	})
	return out
}
