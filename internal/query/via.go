package query

// via: how a change reaches each screen of impact, by the boundaries of shared code the walk
// crosses on the way (docs/decisions/0032).

import (
	"strings"

	"github.com/v-kravchenko/codemap/internal/config"
)

// The ways a change reaches a screen, strongest first: its own code; a navigator it opens other
// screens through; a hub naming many classes of one supertype; a base class it inherits.
const (
	viaDirect = iota
	viaOpener
	viaHub
	viaBase
)

var viaNames = [...]string{"direct", "opener", "hub", "base"}

// screenVia: the strongest way to each screen of w: the walks are rerun crossing fewer boundaries,
// and a screen takes the fewest it needs.
func (g *Graph) screenVia(seeds []Seed, maxDepth int, w *Walk) map[int32]int {
	out := map[int32]int{}
	for _, s := range w.ScreenOrder {
		out[s] = viaBase
	}
	if len(out) == 0 {
		return out
	}
	tops := map[int32]bool{}
	for _, s := range seeds {
		tops[g.topOf(s.N)] = true
	}
	for level := viaHub; level >= viaDirect; level-- {
		cut := func(p Key, c int32) bool { return g.boundary(p, c, tops) > level }
		for s := range g.impactCut(seeds, maxDepth, cut).Screens {
			if _, ok := out[s]; ok {
				out[s] = level
			}
		}
	}
	return out
}

// boundary: the boundary a step from p to c crosses, viaDirect for none. A change to the shared code
// itself (the navigator, the hub, the base class) crosses none.
func (g *Graph) boundary(p Key, c int32, seedTops map[int32]bool) int {
	pt, ct := g.topOf(p.N), g.topOf(c)
	// a hub method, or a navigator: a class with one (a method of it only one screen calls routes like
	// the rest); entered from another file, a change to the navigator itself crossing none
	if g.fileOf(p.N) != g.fileOf(c) && (g.isNavHub(c) && !g.isNavHub(p.N) || g.navClasses[ct] && !g.navClasses[pt]) {
		return viaOpener
	}
	if pt == ct {
		return viaDirect
	}
	if g.kind(c) == "M" && g.isRegistry(c, pt) {
		return viaHub
	}
	// code of a base of many classes reached for no instance in particular, not for this subclass (Recv)
	if p.Recv < 0 && !seedTops[pt] && g.isSubtype(ct, pt) && g.subclassCount(pt) >= config.BaseSubclasses {
		return viaBase
	}
	return viaDirect
}

// isNavHub: a node many screens open others through (the hubs of opened_by); navClasses: the classes
// with one, not a file of top-level functions (its functions share no navigator).
func (g *Graph) isNavHub(n int32) bool {
	if g.navHubs == nil {
		g.navHubs, g.navClasses = map[int32]bool{}, map[int32]bool{}
		for _, h := range g.m.OpenedBy.Hub {
			if h >= 0 {
				g.navHubs[h] = true
				if top := g.topOf(h); top >= 0 && g.kind(top) == "C" && !strings.HasSuffix(g.name(top), "Kt") {
					g.navClasses[top] = true
				}
			}
		}
	}
	return g.navHubs[n]
}

// isRegistry: whether method m names impact.hub_subtypes or more classes below one repo supertype of
// class t (a registry of message types, a serializer module), so that it reads any of them.
func (g *Graph) isRegistry(m, t int32) bool {
	if g.refSups == nil {
		g.refSups = map[int32]map[int32]int{}
	}
	counts, ok := g.refSups[m]
	if !ok {
		counts = map[int32]int{}
		seen := map[int32]bool{}
		for _, u := range g.uses(m) {
			top := g.topOf(u.N)
			if seen[top] || g.kind(top) != "C" {
				continue
			}
			seen[top] = true
			for s := range g.supSet(top) {
				counts[s]++
			}
		}
		g.refSups[m] = counts
	}
	for s := range g.supSet(t) {
		if counts[s] >= config.HubSubtypes {
			return true
		}
	}
	return false
}
