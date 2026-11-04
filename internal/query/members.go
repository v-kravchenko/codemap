package query

// Facts of a symbol and of a use of it, for a consumer that sorts a class's members by how they are
// reached: visibility and overrides of a member; the method a use's code was written in (a lambda
// or a coroutine counts as the method creating it), and what that method is to the symbol.

import (
	"sort"

	"github.com/v-kravchenko/codemap/internal/util"
)

const (
	accPublic    = 0x0001
	accProtected = 0x0004
	accSynthetic = 0x1000
)

// visibility: the JVM's, so a Kotlin `internal` member is public.
func (g *Graph) visibility(n int32) string {
	switch f := g.flags(n); {
	case f&accPublic != 0:
		return "public"
	case f&accProtected != 0:
		return "protected"
	case f&accPrivate != 0:
		return "private"
	}
	return "package"
}

// overridesOf: the repo methods n overrides, qualified, sorted; a library's are not in the map.
func (g *Graph) overridesOf(n int32) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, m := range g.overridden(n) {
		if q := g.qualified(m); !seen[q] {
			seen[q] = true
			out = append(out, q)
		}
	}
	sort.Strings(out)
	return out
}

// sourceMethod: the class and name of the method whose source holds the code of n: a method of a
// source class, or for a lambda, function reference or coroutine the method creating it, by the
// class file's EnclosingMethod or else its compiled name; ok false when that is not known (a class
// or field, a lambda of a field initializer).
func (g *Graph) sourceMethod(n int32) (int32, string, bool) {
	var cls int32
	method := ""
	switch g.kind(n) {
	case "M":
		cls, method = g.owner(n), g.name(n)
	case "C":
		cls = n
	default:
		return -1, "", false
	}
	for range 32 {
		if cls < 0 {
			return -1, "", false
		}
		if outer, m, ok := g.enclosedBy(cls); ok {
			cls, method = outer, m
			continue
		}
		if method == "" {
			return -1, "", false
		}
		// a lambda compiled to a method of its class (`f$lambda$0`, `_init_$lambda$0`), a default-arguments
		// or suspend stub
		if mm := lambdaName.FindStringSubmatch(method); mm != nil {
			method = mm[1]
		}
		method = syntheticSuffix.ReplaceAllString(method, "")
		if method == "_init_" || method == "init" {
			method = "<init>"
		}
		return cls, method, method != ""
	}
	return -1, "", false
}

// enclosedBy: the class and method a compiler-made class was created in; method "" (ok false) when
// that is not known.
func (g *Graph) enclosedBy(cls int32) (int32, string, bool) {
	if g.enclosedIn == nil {
		g.enclosedIn = make(map[int32]int, len(g.m.Enclosing.Cls))
		for i, id := range g.m.Enclosing.Cls {
			g.enclosedIn[g.m.Index(id)] = i
		}
	}
	if r, ok := g.enclosedIn[cls]; ok {
		outer, ok := g.classByName[g.m.Enclosing.Outer.At(r)]
		if !ok {
			return -1, "", true
		}
		return outer, g.m.Enclosing.Method.At(r), true
	}
	name := g.name(cls)
	outer, method, ok := g.enclosing(name)
	if !ok {
		return -1, "", false
	}
	o, found := g.classByName[outer]
	if !found || method == "" {
		return -1, "", true
	}
	return o, method, true
}

// callerFacts: what the code of a use n is to its target: the method it was written in; constructor,
// that method is a constructor or initializer; supertype, its class is a supertype of the target's;
// screens, the screens whose own code it is (a screen class, a composable a route draws). nil when
// the method is not known.
func (g *Graph) callerFacts(n, target int32) obj {
	cls, method, ok := g.sourceMethod(n)
	if !ok {
		return nil
	}
	tc := target
	if g.kind(target) != "C" {
		tc = g.owner(target)
	}
	return obj{
		{"source", util.Dotted(g.name(cls)) + "." + method},
		{"constructor", method == "<init>" || method == "<clinit>"},
		{"supertype", cls != tc && g.isSubtype(tc, cls)},
		{"screens", objs(g.codeScreens(cls, method))},
	}
}

// codeScreens: the screens whose own code a method is: of a screen class (or a class nested in it),
// a composable a route draws, or a private function of its file only those call; as {kind, name},
// sorted.
func (g *Graph) codeScreens(cls int32, method string) []obj {
	if g.drawnBy == nil {
		g.drawnBy = map[int32][]int32{}
		for r, ns := range g.drawn {
			if k := g.screens[r][0].Kind; k != "route" && k != "preview" {
				continue // what an Activity or fragment calls is a fact, not its own code: a shared body is not (ADR 0034)
			}
			for _, n := range ns {
				g.drawnBy[n] = append(g.drawnBy[n], r)
			}
		}
	}
	set := map[int32]bool{}
	if top := g.topOf(cls); g.screenTops[top] {
		set[top] = true
	}
	var stack []int32
	for _, m := range g.members(cls) {
		if g.name(m) == method {
			stack = append(stack, m)
		}
	}
	seen := map[int32]bool{}
	for len(stack) > 0 {
		m := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[m] {
			continue
		}
		seen[m] = true
		if rs := g.drawnBy[m]; len(rs) > 0 {
			for _, r := range rs {
				set[r] = true
			}
			continue
		}
		if g.flags(m)&accPrivate == 0 {
			continue
		}
		// a private function: the screens of its callers in its file, a lambda counting as its method
		for _, u := range edgesOf(&g.m.In, m, nil) {
			if uc, um, ok := g.sourceMethod(u.N); ok && g.fileOf(m) != 0 && g.fileOf(uc) == g.fileOf(m) {
				for _, x := range g.members(uc) {
					if g.name(x) == um {
						stack = append(stack, x)
					}
				}
			}
		}
	}
	var out []obj
	for s := range set {
		for _, row := range g.screens[s] {
			if screenKinds[row.Kind] {
				out = append(out, obj{{"kind", row.Kind}, {"name", row.Label}})
			}
		}
	}
	sortObjs(out, "name", "kind")
	return dedupObjs(out)
}

func dedupObjs(s []obj) []obj {
	out := s[:0]
	for i, o := range s {
		if i > 0 && o.get("name") == s[i-1].get("name") && o.get("kind") == s[i-1].get("kind") {
			continue
		}
		out = append(out, o)
	}
	return out
}
