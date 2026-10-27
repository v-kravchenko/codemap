package query

// Screens a change removes: those of the base's map (--base-db) the map's revision has not
// (docs/decisions/0022).

import "slices"

type removedScreen struct{ kind, name, target, module, location string }

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
