package query

// diff: what one map has and another has not, by node id: screens, the manifest's entries into
// them and the screens that open them. Two variants, or two revisions (docs/decisions/0023).

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/util"
)

type diffEntry struct {
	sid  int32
	kind string
	e    navEntry
}

type diffOpen struct{ from, to int32 }

// mapFacts: a map's screens, entries and opens, keyed by node ids so two maps compare.
type mapFacts struct {
	screens map[int64]int32
	entries map[[3]string]diffEntry
	opens   map[[2]int64]diffOpen
}

func (g *Graph) facts() mapFacts {
	f := mapFacts{map[int64]int32{}, map[[3]string]diffEntry{}, map[[2]int64]diffOpen{}}
	for _, s := range g.screenOrder {
		f.screens[g.id(s)] = s
		for _, e := range g.entriesOf(s) {
			f.entries[[3]string{fmt.Sprint(g.id(s)), e.kind, e.detail}] = diffEntry{s, g.screens[s][0].Kind, e}
		}
	}
	for _, t := range g.openedTargets() {
		ways, _ := g.openedBy(t)
		for _, w := range ways {
			if w.Src >= 0 && len(g.screens[w.Src]) > 0 {
				f.opens[[2]int64{g.id(w.Src), g.id(t)}] = diffOpen{w.Src, t}
			}
		}
	}
	return f
}

// settingsJSON: the settings a map was built with, but its variant: two variants' maps differ by
// it and nothing else should.
func settingsJSON(raw string) string {
	var c config.Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return raw
	}
	c.Variant = ""
	return c.JSON()
}

func (g *Graph) cmdDiff(o opts) int {
	if o.base == "" {
		panic(util.UserError("the following arguments are required: --base FILE"))
	}
	bm, bc := loadMap(o.base, "codemap build --out "+o.base)
	b := NewGraph(bm, g.root)
	var commit any
	if c := bm.MetaValue("git_head"); c != "" {
		commit = c
	}
	g.refs = obj{{"base_map", obj{{"commit", commit}, {"variant", cmp.Or(bc.Variant, "debug")}, {"dirty", bm.MetaValue("dirty") == "1"}}}}
	var warnings []string
	if settingsJSON(bm.MetaValue("config")) != settingsJSON(g.m.MetaValue("config")) {
		msg := "the maps are built with other settings (codemap.yml): screens and opens may differ with no change of code"
		if o.strict {
			fmt.Fprintln(os.Stderr, "error: "+msg)
			return exitStale
		}
		warnings = append(warnings, msg)
	}
	keep := g.inModules(o.modules)
	keepBase := b.inModules(o.modules)
	head, base := g.facts(), b.facts()

	type side struct {
		g    *Graph
		keep func(int32) bool
		f    mapFacts
	}
	only := func(a, other side) (screens []int32, entries []diffEntry, opens []diffOpen) {
		for id, s := range a.f.screens {
			if _, ok := other.f.screens[id]; !ok && a.keep(s) {
				screens = append(screens, s)
			}
		}
		for k, e := range a.f.entries {
			if _, ok := other.f.entries[k]; !ok && a.keep(e.sid) {
				entries = append(entries, e)
			}
		}
		for k, p := range a.f.opens {
			if _, ok := other.f.opens[k]; !ok && (a.keep(p.from) || a.keep(p.to)) {
				opens = append(opens, p)
			}
		}
		ag := a.g
		slices.SortFunc(screens, func(x, y int32) int {
			return slices.Compare([]string{ag.screens[x][0].Kind, ag.module(x), ag.screenName(x), ag.qualified(x)},
				[]string{ag.screens[y][0].Kind, ag.module(y), ag.screenName(y), ag.qualified(y)})
		})
		slices.SortFunc(entries, func(x, y diffEntry) int {
			return slices.Compare([]string{x.e.kind, x.e.detail, ag.screenName(x.sid), ag.qualified(x.sid)},
				[]string{y.e.kind, y.e.detail, ag.screenName(y.sid), ag.qualified(y.sid)})
		})
		slices.SortFunc(opens, func(x, y diffOpen) int {
			return slices.Compare([]string{ag.screenName(x.from), ag.screenName(x.to), ag.qualified(x.from), ag.qualified(x.to)},
				[]string{ag.screenName(y.from), ag.screenName(y.to), ag.qualified(y.from), ag.qualified(y.to)})
		})
		return
	}
	hs, bs := side{g, keep, head}, side{b, keepBase, base}
	addS, addE, addO := only(hs, bs)
	remS, remE, remO := only(bs, hs)
	if o.json {
		screens := func(x *Graph, ns []int32) []any {
			out := []any{}
			for _, n := range ns {
				out = append(out, x.navScreenJSON(n))
			}
			return out
		}
		entries := func(x *Graph, es []diffEntry) []any {
			out := []any{}
			for _, e := range es {
				out = append(out, obj{{"kind", e.e.kind}, {"detail", e.e.detail}, {"screen", x.screenName(e.sid)}, {"module", x.module(e.sid)}})
			}
			return out
		}
		opens := func(x *Graph, ps []diffOpen) []any {
			out := []any{}
			for _, p := range ps {
				out = append(out, obj{{"from", x.screenName(p.from)}, {"to", x.screenName(p.to)},
					{"from_module", x.module(p.from)}, {"to_module", x.module(p.to)}})
			}
			return out
		}
		g.printJSON("diff", obj{
			{"screens", obj{{"added", screens(g, addS)}, {"removed", screens(b, remS)}}},
			{"entries", obj{{"added", entries(g, addE)}, {"removed", entries(b, remE)}}},
			{"opens", obj{{"added", opens(g, addO)}, {"removed", opens(b, remO)}}},
			{"warnings", strs(warnings)},
		})
		return exitOK
	}
	for _, w := range warnings {
		fmt.Println("warning: " + w)
	}
	section := func(title string, added, removed int, line func(sign string, x *Graph, i int)) {
		fmt.Printf("%s: +%d -%d\n", title, added, removed)
		for i := range added {
			line("+", g, i)
		}
		for i := range removed {
			line("-", b, i)
		}
	}
	section("Screens", len(addS), len(remS), func(sign string, x *Graph, i int) {
		n := pick(sign, addS, remS)[i]
		fmt.Println(strings.TrimRight(fmt.Sprintf("  %s %-9s %-50s %-28s %s", sign, x.screens[n][0].Kind, x.screenName(n), x.module(n), x.location(n)), " "))
	})
	section("\nEntries", len(addE), len(remE), func(sign string, x *Graph, i int) {
		e := pick(sign, addE, remE)[i]
		fmt.Printf("  %s %-9s %-50s %s\n", sign, e.e.kind, x.screenName(e.sid), e.e.detail)
	})
	// the opens of an added or removed screen come with it: the text lists those between screens
	// both maps have, and counts the rest
	fmt.Printf("\nOpens: +%d -%d\n", len(addO), len(remO))
	implied := 0
	for _, sd := range []struct {
		sign  string
		x     *Graph
		opens []diffOpen
		gone  []int32
	}{{"+", g, addO, addS}, {"-", b, remO, remS}} {
		for _, p := range sd.opens {
			if slices.Contains(sd.gone, p.from) || slices.Contains(sd.gone, p.to) {
				implied++
				continue
			}
			fmt.Printf("  %s %s -> %s\n", sd.sign, sd.x.screenName(p.from), sd.x.screenName(p.to))
		}
	}
	if implied > 0 {
		fmt.Printf("  (%d more of the added and removed screens; --json lists them)\n", implied)
	}
	return exitOK
}

func pick[T any](sign string, added, removed []T) []T {
	if sign == "+" {
		return added
	}
	return removed
}
