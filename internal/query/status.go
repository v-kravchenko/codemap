package query

// The status of what changes names: a symbol added by the change or modified, a screen new, changed
// itself or affected through what it uses.

import (
	"regexp"
	"strings"
)

// fileBase: a changed file as the base had it, to tell added symbols from modified ones.
type fileBase struct {
	isNew      bool            // not in the base: everything in it is added
	old, cur   string          // the text in the base and in the map's revision
	hasOld     bool            // the base is known (git, or --base)
	plus       lineSet         // the added lines of the diff, for want of a base
	keys       map[string]bool // a values file: the resource keys of the base
	valuesFile bool
}

var lambdaMethod = regexp.MustCompile(`\.([\w$]+)\{λ\}$`)

// declNames: the names a source declares node n by, class or function/property; ok false for code
// the compiler made with no name of its own (a lambda of a class body).
func (g *Graph) declNames(n int32) (names []string, class, ok bool) {
	switch g.kind(n) {
	case "C":
		if _, method, made := g.enclosing(g.name(n)); made {
			if method == "" {
				return nil, false, false
			}
			return []string{method}, false, true
		}
		name := g.name(n)
		return []string{name[strings.LastIndexAny(name, "/$")+1:]}, true, true
	case "M", "F":
		if m := lambdaMethod.FindStringSubmatch(g.makeLabel(n)); m != nil {
			return []string{m[1]}, false, true
		} else if strings.HasSuffix(g.makeLabel(n), "{λ}") {
			return nil, false, false
		}
		name := syntheticSuffix.ReplaceAllString(strings.TrimPrefix(g.sname(n), "access$"), "")
		name, _, _ = strings.Cut(name, "$") // internal: name$module
		switch {
		case name == "<init>":
			return g.declNames(g.owner(n))
		case name == "<clinit>" || name == "":
			return nil, false, false
		}
		names = []string{name}
		for _, p := range []string{"get", "set"} {
			if rest, found := strings.CutPrefix(name, p); found && rest != "" {
				names = append(names, strings.ToLower(rest[:1])+rest[1:])
			}
		}
		return names, false, true
	}
	return nil, false, false
}

// declared: whether text declares one of names (Kotlin or Java, by the file's suffix).
func declared(text string, names []string, class, java bool) bool {
	for _, name := range names {
		q := regexp.QuoteMeta(name)
		var res []string
		switch {
		case class:
			res = []string{`\b(?:class|interface|object|enum|record)\s+` + q + `\b`}
		case java:
			res = []string{`[\w>\]]\s+` + q + `\s*[(=;,]`}
		default:
			res = []string{`\bfun\s+(?:<[^>]*>\s*)?(?:[\w.?<>, ]+\.)?` + q + `\s*\(`,
				`\b(?:val|var)\s+(?:[\w.?<>]+\.)?` + q + `\b`}
		}
		for _, re := range res {
			if regexp.MustCompile(re).MatchString(text) {
				return true
			}
		}
	}
	return false
}

// isAdded: whether the change adds node n: its file is new, or the base does not declare it and the
// map's revision does; without a base, a function all of whose lines the diff adds. A resource: new
// in each file defining it.
func (g *Graph) isAdded(n int32) bool {
	if g.base != nil {
		return g.addedToBase(n)
	}
	if g.bases == nil {
		return false
	}
	if g.kind(n) == "R" {
		rows := 0
		r := &g.m.ResDef
		for i := range r.Node {
			if g.m.Index(r.Node[i]) != n {
				continue
			}
			rows++
			b := g.bases[g.filePath[r.File[i]]]
			if b == nil || !b.isNew && (!b.hasOld || !b.valuesFile || b.keys[g.name(n)]) {
				return false
			}
		}
		return rows > 0
	}
	if !g.isMember(n) {
		return false
	}
	path := g.filePath[g.fileOf(n)]
	b := g.bases[path]
	if b == nil {
		return false
	}
	if b.isNew {
		return true
	}
	if !b.hasOld {
		lo, hi := g.m.Node.LMin[n], g.m.Node.LMax[n]
		if g.kind(n) != "M" || lo == 0 || b.plus == nil {
			return false
		}
		for l := lo; l <= hi; l++ {
			if !b.plus[int(l)] {
				return false
			}
		}
		return true
	}
	if o := g.owner(n); g.kind(n) != "C" && o >= 0 && g.isAdded(o) { // a member of a new class
		return true
	}
	names, class, ok := g.declNames(n)
	java := strings.HasSuffix(path, ".java")
	return ok && declared(b.cur, names, class, java) && !declared(b.old, names, class, java)
}

// addedToBase: whether the map of the base lacks node n (docs/decisions/0022): no node of its id,
// nor a member of its class by its name. A member whose parameters change gets another id but is
// not new; what the compiler numbers (a lambda, an anonymous class) goes with the declaration it
// is in, as one added before it renumbers the rest.
func (g *Graph) addedToBase(n int32) bool {
	if g.base.m.Index(g.m.Node.ID[n]) >= 0 {
		return false
	}
	switch g.kind(n) {
	case "C":
		cls, method, _ := g.enclosing(g.name(n))
		return !g.base.declares(sourceClass(cls), method)
	case "M", "F":
		cls, method, made := g.enclosing(g.name(g.owner(n)))
		if !made && sourceClass(cls) == cls {
			method = memberName(g.sname(n))
		}
		return !g.base.declares(sourceClass(cls), method)
	}
	return true
}

// sourceClass: the class a source declares that a compiler-made one is part of: the file facade
// of its ComposableSingletons.
func sourceClass(name string) string {
	if dir, file, ok := strings.Cut(name, "ComposableSingletons$"); ok {
		return dir + file
	}
	return name
}

// memberName: the name a source gives a method or field by its name in the class: a lambda's
// method's, a synthetic accessor's, a property's (getX: x, its delegate), an internal's (name$module).
func memberName(sname string) string {
	name := syntheticSuffix.ReplaceAllString(strings.TrimPrefix(sname, "access$"), "")
	if m := lambdaName.FindStringSubmatch(name); m != nil {
		name = strings.TrimSuffix(m[1], "_delegate") // the lambda of `by lazy {}`
	}
	name, _, _ = strings.Cut(name, "$")
	for _, p := range []string{"get", "set", "is"} {
		if rest, found := strings.CutPrefix(name, p); found && rest != "" && rest[:1] == strings.ToUpper(rest[:1]) {
			return strings.ToLower(rest[:1]) + rest[1:]
		}
	}
	return name
}

// declares: whether the map has class cls with a member named member ("": the class itself;
// "<clinit>" and enclosing's "init" of property initializers: as old as the class).
func (g *Graph) declares(cls, member string) bool {
	c, ok := g.classByName[cls]
	if !ok {
		return false
	}
	if member == "" || member == "<clinit>" || member == "init" {
		return true
	}
	for _, x := range g.members(c) {
		if memberName(g.sname(x)) == member {
			return true
		}
	}
	return false
}

// changedStatus: of a symbol changes names, nil when the input cannot tell (--files without --base).
func (g *Graph) changedStatus(n int32) any {
	if !g.statusKnown {
		return nil
	}
	if g.isAdded(n) {
		return "added"
	}
	return "modified"
}

// screenStatus: new when the change adds the screen itself, changed when it changes it (depth 0),
// else affected through what it uses.
func (g *Graph) screenStatus(n int32, depth int) string {
	switch {
	case g.statusKnown && g.isAdded(n):
		return "new"
	case depth == 0:
		return "changed"
	}
	return "affected"
}
