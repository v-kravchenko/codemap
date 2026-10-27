package query

// Top-level Kotlin functions moved to another file of their package: the same declaration to
// Kotlin (package, name, parameters), another file facade class to the JVM, so another node id
// (docs/decisions/0025).

import (
	"strings"

	"github.com/v-kravchenko/codemap/internal/snapshot"
)

type moves struct {
	from    map[int32]int32    // a head node -> its node in the base
	to      map[int32]bool     // the base nodes moved
	members map[[2]string]bool // head (facade, function name) of a move: its lambdas are as old
}

// moved: the pairs of top-level functions the base's map has in one file facade and the head's in
// another, each the only one of its package, name and descriptor missing from the other map
// (two `private fun Preview()` of a package moved at once stay added and removed).
func (g *Graph) moved() *moves {
	if g.moves != nil {
		return g.moves
	}
	g.moves = &moves{from: map[int32]int32{}, to: map[int32]bool{}, members: map[[2]string]bool{}}
	if g.base == nil {
		return g.moves
	}
	head, base := g.topLevelMissingFrom(g.base), g.base.topLevelMissingFrom(g)
	for k, h := range head {
		if b, ok := base[k]; ok && len(h) == 1 && len(b) == 1 {
			g.moves.from[h[0]] = b[0]
			g.moves.to[b[0]] = true
			g.moves.members[[2]string{g.name(g.owner(h[0])), g.sname(h[0])}] = true
		}
	}
	return g.moves
}

// topLevelMissingFrom: the top-level functions (static methods of a file facade, `FooKt`) whose
// node id other lacks, by package, name and descriptor.
func (g *Graph) topLevelMissingFrom(other *Graph) map[string][]int32 {
	out := map[string][]int32{}
	for n := range int32(g.m.Nodes()) {
		if g.kind(n) != "M" || g.flags(n)&snapshot.AccStatic == 0 {
			continue
		}
		cls := g.name(g.owner(n))
		if !strings.HasSuffix(cls, "Kt") || strings.Contains(lastSegment(cls), "$") || other.m.Index(g.id(n)) >= 0 {
			continue
		}
		pkg := cls[:strings.LastIndex(cls, "/")+1]
		k := pkg + g.name(n) + g.desc(n)
		out[k] = append(out[k], n)
	}
	return out
}

// movedFrom: of a screen changes names, the screen of the base it was before a move: its name and
// location there; nil when it did not move or without a base map.
func (g *Graph) movedFrom(n int32) any {
	b, ok := g.moved().from[n]
	if !ok {
		return nil
	}
	return obj{{"name", g.base.screens[b][0].Label}, {"location", g.base.location(b)}}
}
