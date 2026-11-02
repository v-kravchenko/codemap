package query

// used-by and uses: the direct uses of a symbol.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/v-kravchenko/codemap/internal/snapshot"
	"github.com/v-kravchenko/codemap/internal/util"
)

func (g *Graph) cmdEdges(cmd string, o opts) int {
	var found []Seed
	seen := map[int32]bool{}
	for _, text := range o.pos {
		for _, s := range g.resolveOne(text, o.all) {
			if !seen[s.N] {
				seen[s.N] = true
				found = append(found, s)
			}
		}
	}
	var rows []EdgeRow
	for _, s := range found {
		if cmd == "used-by" {
			for _, r := range g.directUsers(s.N, s.Mode) {
				r.Target = s.N
				rows = append(rows, r)
			}
			continue
		}
		targets := []int32{s.N}
		if g.kind(s.N) == "C" && s.Mode == modeWhole {
			targets = append(targets, g.members(s.N)...)
		}
		for _, t := range targets {
			for _, u := range g.uses(t) {
				if u.Kind == snapshot.INJECT {
					continue
				}
				if g.topOf(u.N) == g.topOf(s.N) && (s.Mode == modeWhole || g.kind(u.N) == "C") {
					continue // the class's own members, lambdas and Compose `$stable`
				}
				if g.label(u.N) == strings.TrimSuffix(g.label(t), "()")+"{λ}" {
					continue // the method's own lambdas
				}
				rows = append(rows, EdgeRow{u.N, edgeKinds[u.Kind], t, s.N})
			}
		}
	}
	if o.noSelf && cmd == "used-by" {
		kept := rows[:0]
		for _, r := range rows {
			if g.topOf(r.Other) != g.topOf(r.Via) {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	outside := -1
	if mf := g.moduleFilter(o.modules); mf != nil {
		kept, hidden := rows[:0], map[int32]bool{}
		for _, r := range rows {
			if mf.keeps(g.module(r.Other)) {
				kept = append(kept, r)
			} else {
				hidden[r.Other] = true
			}
		}
		rows, outside = kept, len(hidden)
	}
	title := map[string]string{"used-by": "Used by", "uses": "Uses"}[cmd]
	return g.printEdges(found, rows, title, o.json, cmd, outside, o.noDI)
}

// printEdges: outside, the symbols --module left out (-1 without it); noDI leaves out generated DI code,
// still counted in generated_di.
func (g *Graph) printEdges(found []Seed, rows []EdgeRow, title string, asJSON bool, command string, outside int, noDI bool) int {
	isFound := map[int32]bool{}
	for _, s := range found {
		isFound[s.N] = true
	}
	generated := map[int32]bool{}
	kept := rows[:0]
	for _, r := range rows {
		if g.diGlue[g.topOf(r.Other)] {
			generated[r.Other] = true
			if noDI {
				continue
			}
		}
		kept = append(kept, r)
	}
	rows = kept
	type use struct{ other, target int32 }
	groups := map[string]map[use]map[string]bool{}
	var modOrder []string             // first-seen order: ties of the sort below keep it
	othersOrder := map[string][]use{} // first-seen order
	targets := map[int32]bool{}
	for _, r := range rows {
		targets[r.Target] = true
		name := r.Kind
		if !isFound[r.Via] {
			l := g.label(r.Via)
			name += " " + l[strings.LastIndex(l, ".")+1:]
		}
		if generated[r.Other] {
			name += " (generated DI)"
		}
		mod := g.module(r.Other)
		if groups[mod] == nil {
			groups[mod] = map[use]map[string]bool{}
			modOrder = append(modOrder, mod)
		}
		u := use{r.Other, r.Target}
		if groups[mod][u] == nil {
			groups[mod][u] = map[string]bool{}
			othersOrder[mod] = append(othersOrder[mod], u)
		}
		groups[mod][u][name] = true
	}
	if asJSON {
		var symbols []string
		for _, s := range found {
			symbols = append(symbols, g.qualified(s.N))
		}
		sort.Strings(symbols)
		type item struct {
			o                                obj
			module, symbol, location, target string
		}
		var items []item
		for _, mod := range modOrder {
			for _, u := range othersOrder[mod] {
				s := g.symbol(u.other)
				t := g.qualified(u.target)
				s = append(s, kv{"edges", util.SortedKeys(groups[mod][u])}, kv{"target", t})
				if command == "used-by" {
					if f := g.callerFacts(u.other, u.target); f != nil {
						s = append(s, f...)
					}
				}
				items = append(items, item{s, s.get("module"), s.get("symbol"), s.get("location"), t})
			}
		}
		sort.SliceStable(items, func(i, j int) bool {
			a, b := items[i], items[j]
			if a.module != b.module {
				return a.module < b.module
			}
			if a.symbol != b.symbol {
				return a.symbol < b.symbol
			}
			if a.location != b.location {
				return a.location < b.location
			}
			return a.target < b.target
		})
		out := make([]any, len(items))
		for i, it := range items {
			out[i] = it.o
		}
		result := obj{{"symbols", strs(symbols)}, {"items", out}, {"generated_di", len(generated)}}
		if outside >= 0 {
			result = append(result, kv{"hidden_by_module", obj{{"items", outside}}})
		}
		g.printJSON(command, result)
		return exitOK
	}
	for _, s := range found {
		fmt.Println(g.describe(s.N))
	}
	total := 0
	for _, v := range groups {
		total += len(v)
	}
	gen := ""
	if len(generated) > 0 && noDI {
		gen = fmt.Sprintf("; %d generated DI code not shown", len(generated))
	} else if len(generated) > 0 {
		gen = fmt.Sprintf(", %d of them generated DI code (--no-di leaves them out)", len(generated))
	}
	fmt.Printf("%s: %d in %d modules%s\n", title, total, len(groups), gen)
	for _, mod := range util.SortedKeys(groups) {
		fmt.Println("\n" + mod)
		others := othersOrder[mod]
		sort.SliceStable(others, func(i, j int) bool {
			li, lj := g.location(others[i].other), g.location(others[j].other)
			if li != lj {
				return li < lj
			}
			if a, b := g.label(others[i].other), g.label(others[j].other); a != b {
				return a < b
			}
			return g.qualified(others[i].target) < g.qualified(others[j].target)
		})
		for _, u := range others {
			to := ""
			if len(targets) > 1 { // which of the symbols it uses
				to = "  -> " + g.label(u.target)
			}
			fmt.Printf("  %-55s %-22s %s%s\n", g.label(u.other), strings.Join(util.SortedKeys(groups[mod][u]), ", "), g.location(u.other), to)
		}
	}
	if outside > 0 {
		fmt.Println("\n" + outsideLine(fmt.Sprintf("%d symbols", outside)))
	}
	return exitOK
}
