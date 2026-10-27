package query

// What a change adds: a symbol or a screen the map of the base (--base-db) lacks
// (docs/decisions/0022); nothing tells it without one.

import "strings"

// isAdded: whether the change adds node n; false without a base map.
func (g *Graph) isAdded(n int32) bool { return g.base != nil && g.addedToBase(n) }

// addedToBase: whether the map of the base lacks node n (docs/decisions/0022): no node of its id,
// nor a member of its class by its name. A member whose parameters change gets another id but is
// not new; what the compiler numbers (a lambda, an anonymous class) goes with the declaration it
// is in, as one added before it renumbers the rest.
func (g *Graph) addedToBase(n int32) bool {
	if g.base.m.Index(g.m.Node.ID[n]) >= 0 {
		return false
	}
	mv := g.moved()
	if _, ok := mv.from[n]; ok {
		return false
	}
	// a member of a function moved to this file: in the base, in the other file
	declared := func(cls, method string) bool {
		return g.base.declares(cls, method) || mv.members[[2]string{cls, method}]
	}
	switch g.kind(n) {
	case "C":
		cls, method, _ := g.enclosing(g.name(n))
		return !declared(sourceClass(cls), method)
	case "M", "F":
		cls, method, made := g.enclosing(g.name(g.owner(n)))
		if !made && sourceClass(cls) == cls {
			method = memberName(g.sname(n))
		}
		return !declared(sourceClass(cls), method)
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

// changedStatus: of a symbol changes names, nil without a base map.
func (g *Graph) changedStatus(n int32) any {
	if g.base == nil {
		return nil
	}
	if g.isAdded(n) {
		return "added"
	}
	return "modified"
}

// screenAdded: whether the change adds the screen itself, nil without a base map.
func (g *Graph) screenAdded(n int32) any {
	if g.base == nil {
		return nil
	}
	return g.isAdded(n)
}
