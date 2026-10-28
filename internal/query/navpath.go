package query

// nav-path: its ways to a screen through the walk of nav.go, by --via and --ways, as text or --json.

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/util"
)

type navOf struct {
	entry, detail, text string
	cost                int
	path                []string
}

// navOfScreen: the cheapest way to the screen from the launcher, else from its best other entry (impact
// --nav); nil for none.
func (g *Graph) navOfScreen(sid int32) *navOf {
	r := g.navWalk(statesOf([]int32{sid}), navLimits{maxDepth: -1, launcherOnly: true})
	if len(r.roots) == 0 {
		return nil
	}
	root := r.roots[0]
	return &navOf{root.how[0].kind, root.how[0].detail, g.navArrows(r, root.st), r.cost[root.st], g.chainNames(r, root.st)}
}

// unreached: where the walk back from the screen ends when no entry of the manifest reaches it: the
// screens nothing opens or only code outside the screens opens (none when every way needs a host no
// way starts in); false when an entry reaches it. A base class counts as reached: every screen
// extending it opens the screen (a popup BaseActivity shows), and the walk does not go through them.
// reached: the screens known reached, filled as the walks find them: every screen on the way from an
// entry, in no host, starts a walk that finds that entry too.
func (g *Graph) unreached(sid int32, reached map[int32]bool) ([]navRoot, bool) {
	if reached[sid] {
		return nil, false
	}
	r := g.navWalk(statesOf([]int32{sid}), navLimits{maxDepth: -1, anyEntry: true, reached: reached})
	end := r.known
	for _, root := range r.roots {
		if k := root.how[0].kind; end == nil && (k == "base" || slices.Contains(entryKinds, k)) {
			end = &root.st
		}
	}
	if end == nil {
		return r.roots, true
	}
	for _, st := range r.chain(*end) {
		if st.host.none() {
			reached[st.sid] = true
		}
	}
	return nil, false
}

func (n *navOf) json() any {
	if n == nil {
		return nil
	}
	var detail any
	if n.detail != "" {
		detail = n.detail
	}
	return obj{{"entry", n.entry}, {"detail", detail}, {"cost", n.cost}, {"path", strs(n.path)}}
}

// ---------------------------------------------------------------- nav-path

func (g *Graph) cmdNavPath(o opts) int {
	targets := g.screensOf(o.pos[0], o.all)
	lim := navLimits{maxDepth: o.depth, entry: o.entry, avoid: g.screensMatching(o.avoid)}
	if o.from != "" {
		if o.entry != "" {
			panic(util.UserError("argument --from: not allowed with argument --entry"))
		}
		lim.from = map[int32]bool{}
		for _, s := range g.screensOf(o.from, o.all) {
			lim.from[s] = true
		}
	}
	if o.ways && len(o.via) > 0 {
		panic(util.UserError("argument --ways: not allowed with argument --via"))
	}
	t := time.Now()
	switch {
	case len(o.via) > 0:
		via := make([][]int32, len(o.via))
		for i, v := range o.via {
			via[i] = g.screensOf(v, o.all)
		}
		w := g.navVia(targets, via, lim, false)
		g.navViaReport(o, targets, w, time.Since(t))
	case o.ways:
		fork, ways, none := g.navWays(targets, lim)
		g.navWaysReport(o, targets, fork, ways, none, time.Since(t))
	default:
		g.navReport(o, targets, g.navWalk(statesOf(targets), lim), time.Since(t))
	}
	return exitOK
}

// globRe: globs as one regexp (`*`: any run of characters); nil for none.
func globRe(globs []string) *regexp.Regexp {
	if len(globs) == 0 {
		return nil
	}
	var parts []string
	for _, p := range globs {
		parts = append(parts, strings.ReplaceAll(regexp.QuoteMeta(p), `\*`, ".*"))
	}
	return regexp.MustCompile("^(" + strings.Join(parts, "|") + ")$")
}

// inModules: whether a screen is in a module the globs keep (all are without globs).
func (g *Graph) inModules(globs []string) func(int32) bool {
	f := g.moduleFilter(globs)
	return func(n int32) bool { return f.keeps(g.module(n)) }
}

// stepsTo: the fewest steps from each screen to one of the targets over the forward ways.
func (g *Graph) stepsTo(forward map[int32][]fwdWay, targets []int32) map[int32]int {
	back := map[int32][]int32{}
	for _, src := range util.SortedKeys(forward) {
		for _, w := range forward[src] {
			back[w.t] = append(back[w.t], src)
		}
	}
	dist := map[int32]int{}
	level := targets
	for _, t := range targets {
		dist[t] = 0
	}
	for d := 1; len(level) > 0; d++ {
		var nxt []int32
		for _, t := range level {
			for _, src := range back[t] {
				if _, ok := dist[src]; !ok {
					dist[src] = d
					nxt = append(nxt, src)
				}
			}
		}
		level = nxt
	}
	return dist
}

// screensMatching: the screens whose name or class matches one of the globs (`*`: any run of characters).
func (g *Graph) screensMatching(globs []string) map[int32]bool {
	re := globRe(globs)
	if re == nil {
		return nil
	}
	out := map[int32]bool{}
	for n, rows := range g.screens {
		if re.MatchString(rows[0].Label) || re.MatchString(rows[0].Target) {
			out[n] = true
		}
	}
	return out
}

// wayStep: a screen on a way, entry first: its state, the step to the next screen (nil at the
// target), its cost and depth to the target.
type wayStep struct {
	st          navState
	step        *navStep
	cost, depth int
}

// way: the screens from `st` on to the target of the walk.
func (r *navResult) way(st navState) []wayStep {
	var out []wayStep
	for _, x := range r.chain(st) {
		out = append(out, wayStep{x, r.step[x], r.cost[x], r.depth[x]})
	}
	return out
}

// navWay: a way from an entry to the targets; failed: no way, at the screens of --via index
// `failed` (len(via): from the last of them to the targets).
type navWay struct {
	steps   []wayStep
	how     []navEntry
	failed  int
	through int32 // --ways: the screen that opens the fork
}

func (w navWay) cost() int { return w.steps[0].cost }

// navVia: the cheapest way to the targets through the screens of `via`, in order: a walk from the
// targets to the last of them, from there to the one before, and from the first to an entry. Each
// part is the cheapest one, so the way is the cheapest through the state each part ends in. direct: the
// first screen of `via` opens the next one (or the targets) itself.
func (g *Graph) navVia(targets []int32, via [][]int32, lim navLimits, direct bool) navWay {
	starts := statesOf(targets)
	var steps []wayStep
	for k := len(via); k >= 0; k-- {
		part := lim
		if k > 0 {
			part = navLimits{maxDepth: lim.maxDepth, avoid: lim.avoid, from: map[int32]bool{}, direct: direct && k == 1}
			for _, s := range via[k-1] {
				part.from[s] = true
			}
		}
		r := g.navWalk(starts, part)
		if len(r.roots) == 0 {
			return navWay{failed: k, through: -1}
		}
		root := r.roots[0]
		got := r.way(root.st)
		if len(steps) > 0 {
			for i := range got {
				got[i].cost += steps[0].cost
				got[i].depth += steps[0].depth
			}
			steps = append(got[:len(got)-1], steps...)
		} else {
			steps = got
		}
		if k == 0 {
			return navWay{steps, root.how, -1, -1}
		}
		starts = []navState{root.st}
	}
	panic("unreachable")
}

// navWays: the cheapest way through each screen that opens the fork, the first screen back from the
// targets on their cheapest way that more than one screen opens; by entry kind and cost, as the roots
// of nav-path, and the screens that open the fork but no way reaches. Without a fork: the cheapest way.
func (g *Graph) navWays(targets []int32, lim navLimits) (fork int32, out []navWay, none []int32) {
	r := g.navWalk(statesOf(targets), lim)
	if len(r.roots) == 0 {
		return -1, nil, nil
	}
	way := r.way(r.roots[0].st)
	for i := len(way) - 1; i >= 0; i-- {
		fork = way[i].st.sid
		ways, _ := g.openedBy(fork)
		var srcs []int32
		for _, w := range ways {
			if w.Src != fork && !lim.avoid[w.Src] && !containsInt(srcs, w.Src) {
				srcs = append(srcs, w.Src)
			}
		}
		if len(srcs) < 2 {
			continue
		}
		for _, src := range sortedInts(srcs) {
			via := [][]int32{{src}}
			if i < len(way)-1 {
				via = append(via, []int32{fork})
			}
			if w := g.navVia(targets, via, lim, true); w.failed < 0 {
				w.through = src
				out = append(out, w)
			} else {
				none = append(none, src)
			}
		}
		sort.SliceStable(out, func(i, j int) bool {
			if a, b := entryOrder[out[i].how[0].kind], entryOrder[out[j].how[0].kind]; a != b {
				return a < b
			}
			if a, b := out[i].cost(), out[j].cost(); a != b {
				return a < b
			}
			return g.screenName(out[i].through) < g.screenName(out[j].through)
		})
		sort.SliceStable(none, func(i, j int) bool { return g.screenName(none[i]) < g.screenName(none[j]) })
		return fork, out, none
	}
	return -1, []navWay{{way, r.roots[0].how, -1, -1}}, nil
}

func (g *Graph) hostsJSON(hosts []int32) obj {
	names := []string{}
	for _, h := range hosts {
		names = append(names, g.label(h))
	}
	var first any
	if len(names) > 0 {
		first = names[0]
	}
	return obj{{"host", first}, {"hosts", strs(names)}}
}

func (g *Graph) hosted(h hostKey) any {
	if h.node >= 0 {
		return g.label(h.node)
	}
	if h.mod != "" {
		return h.mod
	}
	return nil
}

func (g *Graph) navScreenJSON(sid int32) obj {
	o := obj{{"name", g.screenName(sid)}, {"kind", g.screens[sid][0].Kind}, {"module", g.module(sid)},
		{"location", g.location(sid)}}
	return append(o, g.hostsJSON(g.hosts(sid))...)
}

func (g *Graph) qualifiedOrNil(n int32) any {
	if n < 0 {
		return nil
	}
	return g.qualified(n)
}

func (g *Graph) qualifiedList(ns []int32) []any {
	out := make([]any, len(ns))
	for i, n := range ns {
		out[i] = g.qualified(n)
	}
	return out
}

type branchWay struct {
	c              int
	entry, through string
	st             navState
	src, hub       int32
}

func (g *Graph) navReport(o opts, targets []int32, r *navResult, elapsed time.Duration) {
	branchWays := g.branchWays(r)
	switch {
	case o.json:
		g.navJSON(o, targets, r, branchWays)
	case !o.verbose:
		g.navCheapest(targets, r, branchWays, elapsed)
	default:
		g.navVerbose(o, targets, r, branchWays, elapsed)
	}
}

// branchWays: the cheapest way through each screen a launcher opens (not one coming back to the launcher).
func (g *Graph) branchWays(r *navResult) []branchWay {
	var branchWays []branchWay
	for _, b := range r.branches {
		back := false
		for _, y := range r.chain(b.st) {
			back = back || y.sid == b.src
		}
		if !back {
			branchWays = append(branchWays, branchWay{b.cost, g.screenName(b.src), g.screenName(b.st.sid), b.st, b.src, b.hub})
		}
	}
	sort.SliceStable(branchWays, func(i, j int) bool {
		a, b := branchWays[i], branchWays[j]
		if a.c != b.c {
			return a.c < b.c
		}
		if a.entry != b.entry {
			return a.entry < b.entry
		}
		return a.through < b.through
	})
	return branchWays
}

func (g *Graph) navJSON(o opts, targets []int32, r *navResult, branchWays []branchWay) {
	onWays := map[navState]bool{}
	if o.verbose {
		for st := range r.step {
			onWays[st] = true
		}
	} else {
		for _, root := range r.roots {
			for _, x := range r.chain(root.st) {
				onWays[x] = true
			}
		}
		for _, b := range branchWays {
			for _, x := range r.chain(b.st) {
				onWays[x] = true
			}
		}
	}
	type item struct {
		o                  obj
		cost               int
		name, mod, hostStr string
	}
	var items []item
	for _, st := range r.stepOrder {
		s := r.step[st]
		if s == nil || !onWays[st] {
			continue
		}
		hs, _ := g.hosted(st.host).(string)
		o := g.stepJSON(wayStep{st, s, r.cost[st], r.depth[st]})
		items = append(items, item{o, r.cost[st], g.screenName(st.sid), g.module(st.sid), hs})
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.cost != b.cost {
			return a.cost < b.cost
		}
		if a.name != b.name {
			return a.name < b.name
		}
		if a.mod != b.mod {
			return a.mod < b.mod
		}
		return a.hostStr < b.hostStr
	})
	screens := make([]any, len(items))
	for i, it := range items {
		screens[i] = it.o
	}
	targetsJSON := make([]any, len(targets))
	for i, s := range targets {
		targetsJSON[i] = g.navScreenJSON(s)
	}
	var entries, paths []any
	for _, root := range r.roots {
		entries = append(entries, g.entryJSON(root.sid, root.how))
		paths = append(paths, strs(g.chainNames(r, root.st)))
	}
	var branches []any
	for _, b := range branchWays {
		branches = append(branches, obj{{"entry", b.entry}, {"through", b.through}, {"cost", b.c},
			{"shared", g.qualifiedOrNil(b.hub)}, {"path", strs(append([]string{b.entry}, g.chainNames(r, b.st)...))}})
	}
	result := obj{{"targets", list(targetsJSON)}, {"screens", list(screens)}, {"entries", list(entries)},
		{"paths", list(paths)}, {"branches", list(branches)}}
	if o.verbose {
		var hubs, bases []obj
		for _, h := range r.hubs {
			from := make([]string, len(h.below))
			for i, s := range h.below {
				from[i] = g.screenName(s)
			}
			sort.Strings(from)
			hubs = append(hubs, obj{{"opens", g.screenName(h.sid)}, {"via", g.qualified(h.hub)}, {"from", strs(from)}})
		}
		sortObjs(hubs, "opens", "via")
		for _, b := range r.bases {
			bases = append(bases, obj{{"opens", g.screenName(b.sid)}, {"base", g.qualified(b.hub)},
				{"subclasses", g.subclassCount(b.hub)}, {"screens", len(b.below)}})
		}
		sortObjs(bases, "opens", "base")
		result = append(result, kv{"hubs", objs(hubs)}, kv{"bases", objs(bases)})
	}
	g.printJSON("nav-path", result)
}

// navCheapest: the text of nav-path: the cheapest way and where others join it.
func (g *Graph) navCheapest(targets []int32, r *navResult, branchWays []branchWay, elapsed time.Duration) {
	targetNames := make([]string, len(targets))
	for i, s := range targets {
		targetNames[i] = g.screenName(s)
	}
	arrows := func(st navState) string { return g.navArrows(r, st) }
	if len(r.roots) == 0 {
		fmt.Printf("No way to %s found (as far as the code map sees).\n", strings.Join(targetNames, ", "))
		return
	}
	root := r.roots[0]
	st := root.st
	fmt.Printf("Cheapest way to %s%s:\n", strings.Join(targetNames, ", "), g.entrySuffix(root.sid, root.how))
	g.printWay(r.way(st))
	names := g.chainNames(r, st)
	var joinOrder []string
	joins := map[string][]string{} // join screen -> [first screen of the branch (screens before the join)]
	for _, b := range branchWays {
		path := append([]string{b.entry}, g.chainNames(r, b.st)...)
		if slicesEqual(path, names) || b.c > r.cost[st]+config.BranchSlack {
			continue
		}
		before, join := joined(names, path)
		if len(before) > 1 && before[0] == names[0] {
			if _, ok := joins[join]; !ok {
				joinOrder = append(joinOrder, join)
			}
			s := ""
			if len(before) > 2 {
				s = "s"
			}
			joins[join] = append(joins[join], fmt.Sprintf("%s (%d screen%s)", before[1], len(before)-1, s))
		} else {
			fmt.Println("  also: " + b.entry + " -> " + arrows(b.st))
		}
	}
	for _, join := range joinOrder {
		fmt.Printf("  %s also after: %s\n", join, strings.Join(joins[join], ", "))
	}
	// the deep link or intent filter that joins this way soonest, up to the screen it joins at
	best, bestN, bestC := -1, 0, 0
	var bestJoin string
	for i, root := range r.roots[1:] {
		i++
		if k := root.how[0].kind; k == "deeplink" || k == "intent" {
			before, join := joined(names, g.chainNames(r, root.st))
			if n, c := len(before), r.cost[root.st]; best < 0 || n < bestN || n == bestN && c < bestC {
				best, bestN, bestC, bestJoin = i, n, c, join
			}
		}
	}
	if best >= 0 && len(names) > 1 && root.how[0].kind != "from" {
		root := r.roots[best]
		text := entryText(root.how[0])
		if len(root.how) > 1 {
			text += fmt.Sprintf(" (+%d)", len(root.how)-1)
		}
		parts := strings.Split(arrows(root.st), " ")
		fmt.Printf("  %s also from %s: %s\n", bestJoin, text, strings.Join(parts[:min(2*bestN+1, len(parts))], " "))
	}
	fmt.Printf("(%.1fs; ~push~>: only for a notification or a deep link; every way and the code of each step: --verbose)\n",
		elapsed.Seconds())
}

// navVerbose: the text of nav-path --verbose: every way found and the code of each step.
func (g *Graph) navVerbose(o opts, targets []int32, r *navResult, branchWays []branchWay, elapsed time.Duration) {
	limit := o.limit
	if limit < 0 {
		limit = 20
	}
	arrows := func(st navState) string { return g.navArrows(r, st) }
	for _, sid := range targets {
		fmt.Printf("%s  %s  %s\n", g.screenName(sid), g.screens[sid][0].Kind, g.module(sid))
	}
	onWay := map[int32]bool{}
	for st := range r.step {
		onWay[st.sid] = true
	}
	fmt.Printf("\nScreens on the way: %d, entry screens: %d\n", len(onWay)-len(targets), len(r.roots))
	shown := r.roots[:min(limit, len(r.roots))]
	first := ""
	if len(shown) < len(r.roots) {
		first = fmt.Sprintf(" (first %d)", len(shown))
	}
	fmt.Printf("\nWays from entry screens, cheapest first%s:\n", first)
	for _, root := range shown {
		var parts []string
		for i, e := range root.how {
			if i == 3 {
				break
			}
			parts = append(parts, entryText(e))
		}
		text := strings.Join(parts, ", ")
		if len(root.how) > 3 {
			text += ", ..."
		}
		fmt.Printf("  [%s] %s\n", text, arrows(root.st))
	}
	fmt.Println("  entry kinds: launcher, deeplink, intent (manifest); push: the code of a service taking a push\n" +
		"  opens it; base: only a base class of many screens opens it; outside: opened from code that is not\n" +
		"  a screen; unknown: the map\n" +
		"  sees nothing that opens it. ~push~>: the step goes through push or deep link handling; (in M): a shared\n" +
		"  screen, this way only when a screen of module M hosts it")
	if len(branchWays) > 0 {
		fmt.Println("\nThe cheapest way through each branch of the launcher (cost, way):")
		var lines []string
		for _, b := range branchWays {
			shared := ""
			if b.hub >= 0 {
				shared = "  (shared: " + g.label(b.hub) + ")"
			}
			lines = append(lines, fmt.Sprintf("  %-3d %s%s", b.c, b.entry+" -> "+arrows(b.st), shared))
		}
		fmt.Println(strings.Join(lines, "\n"))
	}
	fmt.Println("\nOpened from (cost, screen -> what it opens: own code, shared code or push handling):")
	var states []navState
	for _, st := range r.stepOrder {
		if r.step[st] != nil {
			states = append(states, st)
		}
	}
	sort.SliceStable(states, func(i, j int) bool {
		a, b := states[i], states[j]
		if r.cost[a] != r.cost[b] {
			return r.cost[a] < r.cost[b]
		}
		if na, nb := g.screenName(a.sid), g.screenName(b.sid); na != nb {
			return na < nb
		}
		if a.host.node >= 0 && b.host.node >= 0 {
			return a.host.node < b.host.node
		}
		return a.host.mod < b.host.mod
	})
	for _, st := range states {
		s := r.step[st]
		name := g.screenName(st.sid)
		if h := g.hosted(st.host); h != nil {
			name += " (in " + h.(string) + ")"
		}
		fmt.Printf("  %-3d %-45s -> %s  [%s]\n", r.cost[st], name, g.screenName(s.opens.sid), stepWay(s))
		if !o.noPaths {
			labels := make([]string, len(s.via))
			for i, n := range s.via {
				labels[i] = g.label(n)
			}
			fmt.Println("        " + strings.Join(labels, " <- "))
		}
	}
	g.navHubsText(r)
	fmt.Printf("(%.2fs)\n", elapsed.Seconds())
}

// navHubsText: the shared code and the base classes the ways go through.
func (g *Graph) navHubsText(r *navResult) {
	byScreenLabel := func(s []navHub) []navHub {
		s = append([]navHub(nil), s...)
		sort.SliceStable(s, func(i, j int) bool {
			if a, b := g.screenName(s[i].sid), g.screenName(s[j].sid); a != b {
				return a < b
			}
			return g.label(s[i].hub) < g.label(s[j].hub)
		})
		return s
	}
	if len(r.hubs) > 0 {
		fmt.Printf("\nShared code (each screen below costs %d more):\n", config.StepShared-config.StepOwn)
		for _, h := range byScreenLabel(r.hubs) {
			var names []string
			for i, s := range h.below {
				if i == 5 {
					break
				}
				names = append(names, g.screenName(s))
			}
			more := ""
			if len(h.below) > 5 {
				more = ", ..."
			}
			fmt.Printf("  %s <- %s: %d screens (%s%s)\n", g.screenName(h.sid), g.label(h.hub), len(h.below), strings.Join(names, ", "), more)
		}
	}
	if len(r.bases) > 0 {
		fmt.Println("\nBase classes (any screen extending one opens it, not walked):")
		for _, b := range byScreenLabel(r.bases) {
			fmt.Printf("  %s <- %s: %d classes, %d screens\n", g.screenName(b.sid), g.label(b.hub), g.subclassCount(b.hub), len(b.below))
		}
	}
}

// entrySuffix: how the way starts, after "Cheapest way to X": ` (launcher)`, ` from Y`.
func (g *Graph) entrySuffix(sid int32, how []navEntry) string {
	if how[0].kind == "from" {
		return " from " + g.screenName(sid)
	}
	return " (" + entryText(how[0]) + ")"
}

func stepWay(s *navStep) string {
	out := "own"
	if s.hub >= 0 {
		out = "shared"
	}
	if s.push {
		out = "push " + out
	}
	return out
}

// stepJSON: a screen on a way and the step it takes to the next one.
func (g *Graph) stepJSON(w wayStep) obj {
	s := w.step
	return append(g.navScreenJSON(w.st.sid), kv{"depth", w.depth}, kv{"cost", w.cost}, kv{"hosted_in", g.hosted(w.st.host)},
		kv{"opens", g.screenName(s.opens.sid)}, kv{"way", stepWay(s)}, kv{"shared", g.qualifiedOrNil(s.hub)},
		kv{"hint", g.stepHint(s.via)}, kv{"via", g.qualifiedList(s.via)})
}

// entryJSON: an entry screen and how the system starts it.
func (g *Graph) entryJSON(sid int32, how []navEntry) obj {
	var out []any
	for _, e := range how {
		var d any
		if e.detail != "" {
			d = e.detail
		}
		out = append(out, obj{{"kind", e.kind}, {"detail", d}})
	}
	return append(g.navScreenJSON(sid), kv{"entry", list(out)})
}

func (g *Graph) wayNames(steps []wayStep) []string {
	out := make([]string, len(steps))
	for i, w := range steps {
		out[i] = g.screenName(w.st.sid)
	}
	return out
}

func (g *Graph) wayArrows(steps []wayStep) string {
	out := []string{g.screenName(steps[0].st.sid)}
	for i, w := range steps[1:] {
		arrow := "-> "
		if steps[i].step.push {
			arrow = "~push~> "
		}
		out = append(out, arrow+g.screenName(w.st.sid))
	}
	return strings.Join(out, " ")
}

// printWay: a screen a line, each with what on it opens the next one.
func (g *Graph) printWay(steps []wayStep) {
	names := g.wayNames(steps)
	width := 0
	for _, n := range names {
		width = max(width, utf8.RuneCountInString(n))
	}
	width += 3
	for i, w := range steps {
		arrow := ""
		if i > 0 {
			arrow = "-> "
			if steps[i-1].step.push {
				arrow = "~push~> "
			}
		}
		hint := ""
		if w.step != nil {
			hint = g.stepHint(w.step.via)
		}
		fmt.Println(strings.TrimRight(fmt.Sprintf("  %-*s %s", width+8, arrow+names[i], hint), " "))
	}
}

// navViaReport: nav-path --via: the one way, in the shape of the nav-path answer.
func (g *Graph) navViaReport(o opts, targets []int32, w navWay, elapsed time.Duration) {
	targetNames := make([]string, len(targets))
	for i, s := range targets {
		targetNames[i] = g.screenName(s)
	}
	via := o.via
	if o.json {
		targetsJSON := make([]any, len(targets))
		for i, s := range targets {
			targetsJSON[i] = g.navScreenJSON(s)
		}
		var screens, entries, paths []any
		if w.failed < 0 {
			for i := len(w.steps) - 2; i >= 0; i-- { // cheapest first, as without --via
				screens = append(screens, g.stepJSON(w.steps[i]))
			}
			entries = []any{g.entryJSON(w.steps[0].st.sid, w.how)}
			paths = []any{strs(g.wayNames(w.steps))}
		}
		g.printJSON("nav-path", obj{{"targets", list(targetsJSON)}, {"via", strs(via)}, {"screens", list(screens)},
			{"entries", list(entries)}, {"paths", list(paths)}, {"branches", list(nil)}})
		return
	}
	to := strings.Join(targetNames, ", ") + " via " + strings.Join(via, ", ")
	if w.failed >= 0 {
		why := "no entry reaches " + via[0]
		if w.failed == len(via) {
			why = via[len(via)-1] + " does not lead to " + strings.Join(targetNames, ", ")
		} else if w.failed > 0 {
			why = via[w.failed-1] + " does not lead to " + via[w.failed]
		}
		fmt.Printf("No way to %s found (as far as the code map sees): %s.\n", to, why)
		return
	}
	fmt.Printf("Cheapest way to %s%s:\n", to, g.entrySuffix(w.steps[0].st.sid, w.how))
	g.printWay(w.steps)
	fmt.Printf("(%.1fs; cost %d; ~push~>: only for a notification or a deep link)\n", elapsed.Seconds(), w.cost())
}

// navWaysReport: nav-path --ways: the cheapest way through each screen that opens the fork.
func (g *Graph) navWaysReport(o opts, targets []int32, fork int32, ways []navWay, none []int32, elapsed time.Duration) {
	targetNames := make([]string, len(targets))
	for i, s := range targets {
		targetNames[i] = g.screenName(s)
	}
	if o.json {
		targetsJSON := make([]any, len(targets))
		for i, s := range targets {
			targetsJSON[i] = g.navScreenJSON(s)
		}
		var out []any
		for _, w := range ways {
			var through any
			if w.through >= 0 {
				through = g.screenName(w.through)
			}
			out = append(out, obj{{"through", through}, {"cost", w.cost()},
				{"entry", g.entryJSON(w.steps[0].st.sid, w.how)}, {"path", strs(g.wayNames(w.steps))}})
		}
		noWay := make([]string, len(none))
		for i, s := range none {
			noWay[i] = g.screenName(s)
		}
		var forkName any
		if fork >= 0 {
			forkName = g.screenName(fork)
		}
		g.printJSON("nav-path", obj{{"targets", list(targetsJSON)}, {"fork", forkName}, {"ways", list(out)},
			{"no_way", strs(noWay)}})
		return
	}
	to := strings.Join(targetNames, ", ")
	if len(ways) == 0 && len(none) == 0 {
		fmt.Printf("No way to %s found (as far as the code map sees).\n", to)
		return
	}
	if fork < 0 {
		fmt.Printf("One way to %s, no screen on it is opened from more than one:\n", to)
	} else {
		fmt.Printf("Ways to %s, the cheapest through each screen that opens %s:\n", to, g.screenName(fork))
	}
	limit := o.limit
	if limit < 0 {
		limit = 20
	}
	for _, w := range ways[:min(limit, len(ways))] {
		fmt.Printf("  %-3d [%s] %s\n", w.cost(), entryText(w.how[0]), g.wayArrows(w.steps))
	}
	if len(ways) > limit {
		fmt.Printf("  ... %d more (--limit)\n", len(ways)-limit)
	}
	if len(none) > 0 {
		names := make([]string, len(none))
		for i, s := range none {
			names[i] = g.screenName(s)
		}
		fmt.Printf("  no way to: %s\n", strings.Join(names, ", "))
	}
	fmt.Printf("(%.1fs; the code of each step: --via SCREEN)\n", elapsed.Seconds())
}

func entryText(e navEntry) string {
	if e.detail != "" {
		return e.kind + " " + e.detail
	}
	return e.kind
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- graph
