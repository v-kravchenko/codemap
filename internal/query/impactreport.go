package query

// impact: the answer of the walk of impact.go, up to the screens, as text or --json; changes uses it too.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/util"
)

func shownKinds(o opts) map[string]bool {
	if len(o.kinds) == 0 {
		return screenKinds
	}
	out := map[string]bool{}
	for _, k := range o.kinds {
		if k == "all" {
			for _, t := range config.ScreenTitles {
				out[t[0]] = true
			}
		}
		out[k] = true
	}
	return out
}

func (g *Graph) cmdImpact(o opts) int {
	var seeds []Seed
	for _, s := range o.pos {
		seeds = append(seeds, g.resolveOne(s, o.all)...)
	}
	if o.to != "" {
		if len(o.modules) > 0 {
			panic(util.UserError("argument --module: not allowed with argument --to"))
		}
		return g.impactTo(seeds, o)
	}
	return g.reportImpact(seeds, o, "impact")
}

type screenItem struct {
	depth              int
	label, target, mod string
	hosts              []int32
	n                  int32
}

// openedFromNames: the screens opening a dialog, by name (listed for dialogs only).
func (g *Graph) openedFromNames(n int32) []string {
	names := []string{}
	if g.screens[n][0].Kind != "dialog" {
		return names
	}
	for _, s := range g.openedFrom(n) {
		names = append(names, g.screenName(s))
	}
	sort.Strings(names)
	return names
}

func (g *Graph) reportImpact(seeds []Seed, o opts, command string) int {
	return g.reportImpactWith(seeds, o, command, nil, nil)
}

// reportImpactWith: nc, the changes left out; warnings, what may make the result wrong.
// impactAnswer: what reportImpactWith prints, as text or --json.
type impactAnswer struct {
	seeds        []Seed
	seedIDs      map[int32]bool
	w            *Walk
	mf           *moduleFilter
	byKind       map[string][]screenItem
	hidden       map[string]int
	outScreens   int
	outSymbols   map[int32]bool // affected outside --module
	navs         map[int32]*navOf
	notCov       []notCovered
	warnings     []string
	modules      map[string]int // affected symbols per module
	packages     map[string]int
	nearest      map[int32]Key // the key of each affected node nearest to the change
	nearestOrder []int32
	elapsed      time.Duration
	elapsedNav   time.Duration
}

func (g *Graph) reportImpactWith(seeds []Seed, o opts, command string, nc []notCovered, warnings []string) int {
	a := &impactAnswer{seeds: seeds, warnings: warnings, seedIDs: map[int32]bool{},
		mf: g.moduleFilter(o.modules)} // the answer, not the walk: a screen elsewhere is reached through any module
	t := time.Now()
	a.w = newWalk()
	if len(seeds) > 0 {
		a.w = g.impact(seeds, o.depth)
	}
	a.elapsed = time.Since(t)
	g.impactScreens(a, shownKinds(o))
	a.navs = map[int32]*navOf{}
	if o.nav {
		for _, items := range a.byKind {
			for _, it := range items {
				if screenKinds[g.screens[it.n][0].Kind] {
					a.navs[it.n] = g.navOfScreen(it.n)
				}
			}
		}
	}
	a.elapsedNav = time.Since(t) - a.elapsed
	a.notCov = uniqueNotCovered(nc)
	for _, s := range seeds {
		a.seedIDs[s.N] = true
	}
	g.impactSymbols(a)
	if o.json {
		g.impactJSON(a, command)
	} else {
		g.impactText(a, o)
	}
	return exitOK
}

// impactScreens: the screens reached, by kind, nearest first; those of a kind not shown and those
// outside --module counted.
func (g *Graph) impactScreens(a *impactAnswer, shown map[string]bool) {
	w := a.w
	a.byKind, a.hidden = map[string][]screenItem{}, map[string]int{}
	for _, n := range w.ScreenOrder {
		r := g.screens[n][0]
		if !a.mf.keeps(g.module(n)) {
			a.outScreens++
		} else if shown[r.Kind] {
			a.byKind[r.Kind] = append(a.byKind[r.Kind], screenItem{w.Aff[w.Screens[n]].Depth, r.Label, r.Target, g.module(n), g.hosts(n), n})
		} else {
			a.hidden[r.Kind]++
		}
	}
	for _, items := range a.byKind {
		sort.SliceStable(items, func(i, j int) bool {
			a, b := items[i], items[j]
			if a.depth != b.depth {
				return a.depth < b.depth
			}
			if a.label != b.label {
				return a.label < b.label
			}
			if a.target != b.target {
				return a.target < b.target
			}
			return a.mod < b.mod
		})
	}
}

func uniqueNotCovered(nc []notCovered) []notCovered {
	seen := map[notCovered]bool{}
	var out []notCovered
	for _, x := range nc {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].path != out[j].path {
			return out[i].path < out[j].path
		}
		return out[i].reason < out[j].reason
	})
	return out
}

// impactSymbols: the affected symbols within --module, counted by module and package, each by its
// key nearest to the change.
func (g *Graph) impactSymbols(a *impactAnswer) {
	a.outSymbols, a.modules, a.packages, a.nearest = map[int32]bool{}, map[string]int{}, map[string]int{}, map[int32]Key{}
	for _, k := range a.w.Keys {
		if !a.mf.keeps(g.module(k.N)) {
			if !a.seedIDs[k.N] {
				a.outSymbols[k.N] = true
			}
			continue
		}
		a.modules[g.module(k.N)]++
		if p, ok := g.pkg(k.N); ok {
			a.packages[p]++
		}
		old, ok := a.nearest[k.N]
		if !ok {
			a.nearestOrder = append(a.nearestOrder, k.N)
		}
		if !ok || g.before(a.w, k, old) {
			a.nearest[k.N] = k
		}
	}
}

func (g *Graph) impactJSON(a *impactAnswer, command string) {
	w := a.w
	listed := map[int32]bool{}
	var changed []obj
	for _, s := range a.seeds {
		if !listed[s.N] {
			listed[s.N] = true
			changed = append(changed, g.symbol(s.N))
		}
	}
	sortObjs(changed, "symbol", "location", "kind")
	var affected []obj
	for _, n := range a.nearestOrder {
		if a.seedIDs[n] {
			continue
		}
		k := a.nearest[n]
		affected = append(affected, append(g.symbol(n), kv{"depth", w.Aff[k].Depth}, kv{"path", g.qualifiedPath(g.path(w, k))}))
	}
	sort.SliceStable(affected, func(i, j int) bool {
		a, b := affected[i], affected[j]
		if da, db := a.getInt("depth"), b.getInt("depth"); da != db {
			return da < db
		}
		if a.get("symbol") != b.get("symbol") {
			return a.get("symbol") < b.get("symbol")
		}
		return a.get("location") < b.get("location")
	})
	var screens []any
	for _, kind := range util.SortedKeys(a.byKind) {
		for _, it := range a.byKind[kind] {
			hostNames := []string{}
			for _, h := range it.hosts {
				hostNames = append(hostNames, g.label(h))
			}
			var host any
			if len(hostNames) > 0 {
				host = hostNames[0]
			}
			s := obj{{"kind", kind}, {"name", it.label}, {"target", it.target}, {"module", it.mod},
				{"location", g.location(it.n)}, {"host", host}, {"hosts", strs(hostNames)}, {"depth", it.depth},
				{"opened_from", strs(g.openedFromNames(it.n))},
				{"path", g.qualifiedPath(g.path(w, w.Screens[it.n]))}}
			if nav, ok := a.navs[it.n]; ok {
				s = append(s, kv{"nav", nav.json()})
			}
			screens = append(screens, s)
		}
	}
	var ncJSON []any
	for _, x := range a.notCov {
		ncJSON = append(ncJSON, obj{{"path", x.path}, {"reason", x.reason}})
	}
	result := obj{
		{"changed", objs(changed)},
		{"affected", objs(affected)},
		{"screens", list(screens)},
		{"hidden", countsObj(a.hidden)},
		{"modules", countsObj(a.modules)},
		{"packages", countsObj(a.packages)},
		{"not_covered", list(ncJSON)},
		{"warnings", strs(a.warnings)},
	}
	if a.mf != nil {
		result = append(result, kv{"hidden_by_module", obj{{"screens", a.outScreens}, {"affected", len(a.outSymbols)}}})
	}
	g.printJSON(command, result)
}

func (g *Graph) impactText(a *impactAnswer, o opts) {
	if len(a.seeds) > 0 {
		fmt.Println("Changed:")
	} else {
		fmt.Println("Changed: nothing the code map analyses")
	}
	for _, s := range a.seeds {
		whole := ""
		if s.Mode == modeWhole && g.kind(s.N) == "C" {
			whole = "  (whole class)"
		}
		fmt.Printf("  %s%s\n", g.describe(s.N), whole)
	}
	for _, w := range a.warnings {
		fmt.Println("  warning: " + w)
	}
	total := 0
	var counts []string
	for _, t := range config.ScreenTitles {
		total += len(a.byKind[t[0]])
		if len(a.byKind[t[0]]) > 0 {
			counts = append(counts, fmt.Sprintf("%s %d", t[0], len(a.byKind[t[0]])))
		}
	}
	fmt.Printf("\nScreens: %d  (%s)\n", total, strings.Join(counts, ", "))
	for _, t := range config.ScreenTitles {
		items := a.byKind[t[0]]
		if len(items) == 0 {
			continue
		}
		fmt.Println("\n" + t[1])
		for _, it := range items {
			g.impactScreenText(a, o, t[0], it)
		}
	}
	if len(a.hidden) > 0 {
		fmt.Println("\n" + hiddenLine(a.hidden))
	}
	if a.outScreens+len(a.outSymbols) > 0 {
		fmt.Println("\n" + outsideLine(fmt.Sprintf("%d screens", a.outScreens), fmt.Sprintf("%d symbols", len(a.outSymbols))))
	}
	if len(a.modules) > 0 {
		mods := util.SortedKeys(a.modules)
		sort.SliceStable(mods, func(i, j int) bool { return a.modules[mods[i]] > a.modules[mods[j]] })
		var parts []string
		for _, m := range mods {
			parts = append(parts, fmt.Sprintf("%s %d", m, a.modules[m]))
		}
		fmt.Println("\nModules (affected symbols): " + strings.Join(parts, ", "))
	}
	if len(a.notCov) > 0 {
		notCoveredText(a.notCov)
	}
	nav := ""
	if o.nav {
		nav = fmt.Sprintf(", nav %.2fs", a.elapsedNav.Seconds())
	}
	fmt.Printf("(%d symbols affected, %.2fs%s)\n", len(a.w.Keys), a.elapsed.Seconds(), nav)
}

func (g *Graph) impactScreenText(a *impactAnswer, o opts, kind string, it screenItem) {
	extra := ""
	if len(it.hosts) > 0 {
		var hl []string
		for _, h := range it.hosts {
			hl = append(hl, g.label(h))
		}
		extra = "  host " + strings.Join(hl, ", ")
	}
	name := it.label
	if kind == "preview" {
		name = it.target
	}
	fmt.Println(strings.TrimRight(fmt.Sprintf("  %-50s %-28s%s", name, it.mod, extra), " "))
	if g.screens[it.n][0].Kind == "dialog" {
		names := g.openedFromNames(it.n)
		if len(names) == 0 {
			names = []string{"not found"}
		}
		fmt.Println("      opened from: " + strings.Join(names, ", "))
	}
	if nav, ok := a.navs[it.n]; ok {
		if nav == nil {
			fmt.Println("      nav: not found")
		} else if nav.entry == "launcher" {
			fmt.Println("      nav: " + nav.text)
		} else {
			fmt.Printf("      nav (%s): %s\n", nav.entry, nav.text)
		}
	}
	if !o.noPaths {
		fmt.Println("      " + g.formatPath(g.path(a.w, a.w.Screens[it.n])))
	}
}

func notCoveredText(notCov []notCovered) {
	fmt.Println("\nNot covered:")
	for _, t := range notCoveredTitles {
		var paths []string
		for _, x := range notCov {
			if x.reason == t[0] {
				paths = append(paths, x.path)
			}
		}
		if len(paths) == 0 {
			continue
		}
		more := ""
		if len(paths) > 15 {
			paths, more = paths[:15], fmt.Sprintf(" ... %d more", len(paths)-15)
		}
		fmt.Printf("  %s: %s%s\n", t[1], strings.Join(paths, ", "), more)
	}
}

func (g *Graph) qualifiedPath(path []Step) []any {
	out := make([]any, len(path))
	for i, s := range path {
		out[i] = g.qualified(s.N)
	}
	return out
}

// sourceLabel: a node as the source names it: a lambda as the method holding it.
func (g *Graph) sourceLabel(n int32) string {
	l := g.label(n)
	if strings.HasSuffix(l, "{λ}") {
		l = strings.TrimSuffix(l, "{λ}")
		if strings.Contains(l, ".") {
			l += "()"
		}
	}
	return l
}

// formatPath: a walk path for reading.
func (g *Graph) formatPath(path []Step) string {
	type st struct{ text, full string }
	var steps []st
	owner := ""
	hasOwner := false
	for _, s := range path {
		if g.kind(s.N) == "ROUTE" && len(steps) > 0 {
			break
		}
		l := g.sourceLabel(s.N)
		if len(steps) > 0 && steps[len(steps)-1].full == l {
			continue
		}
		if len(steps) > 0 && steps[len(steps)-1].full == l+".<init>()" {
			steps = steps[:len(steps)-1]
		}
		cls, member := l, ""
		if strings.HasSuffix(l, ")") && strings.Contains(l, ".") {
			b := l[:len(l)-2]
			i := strings.LastIndex(b, ".")
			if i >= 0 {
				cls, member = b[:i], b[i+1:]
			} else {
				cls = b
			}
		}
		text := l
		if member != "" && hasOwner && cls == owner {
			text = "." + member + "()"
		}
		steps = append(steps, st{text, l})
		owner, hasOwner = cls, true
	}
	parts := make([]string, len(steps))
	for i, s := range steps {
		parts[i] = s.text
	}
	return strings.Join(parts, " -> ")
}

func whyOf(path []Step, n int32) string {
	for i, s := range path {
		if s.N == n && i+1 < len(path) {
			return path[i+1].Why
		}
	}
	return ""
}

// impactTo: the shortest path from the change to a screen named TARGET, else to the symbol TARGET or
// code of its top-level class.
func (g *Graph) impactTo(seeds []Seed, o opts) int {
	w := g.impact(seeds, o.depth)
	var named []int32
	for _, n := range g.screenOrder {
		r := g.screens[n][0]
		if o.to == r.Label || o.to == r.Target {
			named = append(named, n)
		}
	}
	var hit *Key
	if len(named) > 0 {
		for _, n := range named {
			if k, ok := w.Screens[n]; ok && (hit == nil || g.before(w, k, *hit)) {
				hit = &k
			}
		}
	} else {
		sources, tops := map[int32]bool{}, map[int32]bool{}
		for _, s := range g.resolveOne(o.to, o.all) {
			sources[s.N] = true
			tops[g.topOf(s.N)] = true
		}
		for _, k := range w.Keys {
			if sources[k.N] || g.isMember(k.N) && tops[g.topOf(k.N)] {
				if hit == nil || g.before(w, k, *hit) {
					k := k
					hit = &k
				}
			}
		}
	}
	var path []Step
	if hit != nil {
		path = g.path(w, *hit)
	}
	changed := strings.Join(o.pos, ", ")
	if o.json {
		set := map[string]bool{}
		for _, s := range seeds {
			set[g.qualified(s.N)] = true
		}
		var steps []any
		for i := len(path) - 1; i >= 0; i-- {
			n := path[i].N
			var edge any
			if e := whyOf(path, n); e != "" {
				edge = e
			}
			steps = append(steps, append(g.symbol(n), kv{"edge", edge}))
		}
		g.printJSON("impact", obj{{"to", o.to}, {"changed", strs(util.SortedKeys(set))}, {"steps", list(steps)}})
		return exitOK
	}
	if hit == nil {
		fmt.Printf("No dependency path: %s does not depend on %s (as far as the code map sees).\n", o.to, changed)
		return exitOK
	}
	fmt.Printf("%s depends on %s in %d steps:\n", o.to, changed, len(path)-1)
	for i := len(path) - 1; i >= 0; i-- {
		n := path[i].N
		prefix := ""
		if i != len(path)-1 {
			prefix = "<-[" + whyOf(path, n) + "]- "
		}
		fmt.Printf("  %s%-50s %s\n", prefix, g.label(n), g.location(n))
	}
	return exitOK
}
