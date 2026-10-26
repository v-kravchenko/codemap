package query

// How the user gets to a screen (nav-path, impact --nav) and what a screen opens (graph): walks over
// the opened_by rows build wrote.

import (
	"container/heap"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/util"
)

var entryOrder = map[string]int{"launcher": 0, "deeplink": 1, "intent": 2, "base": 3, "outside": 4, "unknown": 5}

// hostKey: the host a way needs: a node (an Activity hosting a route), a module, or neither.
type hostKey struct {
	node int32
	mod  string
}

var noHost = hostKey{-1, ""}

func (h hostKey) none() bool { return h.node < 0 && h.mod == "" }

func (w Way) host() hostKey { return hostKey{w.HostNode, w.HostModule} }

type srcWay struct {
	Src int32
	Way
}

type baseBelow struct {
	Base  int32
	Below []int32
}

type openedRows struct {
	ways  []srcWay
	bases []baseBelow
}

// openedBy: the rows build wrote for the screens a user sees, a walk for others.
func (g *Graph) openedBy(sid int32) ([]srcWay, []baseBelow) {
	if g.opened == nil {
		g.opened = map[int32]openedRows{}
	}
	if r, ok := g.opened[sid]; ok {
		return r.ways, r.bases
	}
	var r openedRows
	if screenKinds[g.screens[sid][0].Kind] {
		o := &g.m.OpenedBy
		lo := sort.Search(len(o.Screen), func(i int) bool { return o.Screen[i] >= sid })
		for i := lo; i < len(o.Screen) && o.Screen[i] == sid; i++ {
			r.ways = append(r.ways, srcWay{o.Src[i], Way{o.Hub[i], o.Via.Node[o.Via.Start[i]:o.Via.Start[i+1]],
				o.HostNode[i], o.HostModule.At(i)}})
		}
		b := &g.m.OpenedBase
		lo = sort.Search(len(b.Screen), func(i int) bool { return b.Screen[i] >= sid })
		for i := lo; i < len(b.Screen) && b.Screen[i] == sid; i++ {
			r.bases = append(r.bases, baseBelow{b.Base[i], b.Below.Node[b.Below.Start[i]:b.Below.Start[i+1]]})
		}
	} else {
		ways, bases, _ := g.walkOpenedBy(sid)
		for _, src := range util.SortedKeys(ways) {
			r.ways = append(r.ways, srcWay{src, ways[src]})
		}
		for _, b := range util.SortedKeys(bases) {
			r.bases = append(r.bases, baseBelow{b, bases[b]})
		}
	}
	g.opened[sid] = r
	return r.ways, r.bases
}

type navEntry struct{ kind, detail string }

// entriesOf: how the system starts the screen (manifest): [(kind, detail)] by kind, detail.
func (g *Graph) entriesOf(sid int32) []navEntry {
	if g.entries == nil {
		g.entries = map[int32][]navEntry{}
		e := &g.m.Entry
		for i := range e.Node {
			n := g.m.Index(e.Node[i])
			g.entries[n] = append(g.entries[n], navEntry{e.Kind.At(i), e.Detail.At(i)})
		}
	}
	return g.entries[sid]
}

func (g *Graph) isLauncher(sid int32) bool {
	e := g.entriesOf(sid)
	return len(e) > 0 && e[0].kind == "launcher"
}

// screensOf: screens named `text` (a label or target of the registry), the routes of a sealed class
// named `text`, or else the nearest screens the code of the symbol `text` runs in.
func (g *Graph) screensOf(text string, allowMany bool) []int32 {
	var named []int32
	for n, rows := range g.screens {
		if screenKinds[rows[0].Kind] && (text == rows[0].Label || text == rows[0].Target) {
			named = append(named, n)
		}
	}
	if len(named) == 0 {
		for n, rows := range g.screens {
			if rows[0].Kind == "route" && strings.HasPrefix(rows[0].Label, text+".") ||
				screenKinds[rows[0].Kind] && strings.HasSuffix(rows[0].Target, "."+text) {
				named = append(named, n)
			}
		}
	}
	if len(named) > 0 {
		return sortedInts(named)
	}
	w := g.impact(g.resolveOne(text, allowMany), -1)
	nearest := -1
	for s, k := range w.Screens {
		if d := w.Aff[k].Depth; screenKinds[g.screens[s][0].Kind] && (nearest < 0 || d < nearest) {
			nearest = d
		}
	}
	if nearest < 0 {
		panic(util.UserError(fmt.Sprintf("No screen runs %s (as far as the code map sees)", text)))
	}
	for s, k := range w.Screens {
		if screenKinds[g.screens[s][0].Kind] && w.Aff[k].Depth == nearest {
			named = append(named, s)
		}
	}
	return sortedInts(named)
}

func sortedInts(s []int32) []int32 {
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s
}

// pushWay: whether the code from the opener to the screen goes through push or deep link handling.
func (g *Graph) pushWay(via []int32) bool {
	for _, n := range via[:max(len(via)-1, 0)] {
		if g.kind(n) == "M" && slices.ContainsFunc(config.Cfg.NavPath.PushMarkers, func(s string) bool { return strings.Contains(g.name(n), s) }) {
			return true
		}
	}
	return false
}

// stepHint: what on the screen opens the next one, from the code `via` (opener first): the first two
// methods past the code of screens, routes and Compose functions, else the screen's own method
// nearest the opener, else the navigation resource.
func (g *Graph) stepHint(via []int32) string {
	var found, own []string
	for i := len(via) - 1; i >= 0; i-- {
		n := via[i]
		k := g.kind(n)
		if k == "R" || k == "ROUTE" {
			continue
		}
		label := g.sourceLabel(n)
		top := g.topOf(n)
		if g.screenTops[top] || top >= 0 && strings.HasSuffix(g.name(top), "Kt") {
			if k == "M" && !strings.HasSuffix(label, ".<init>()") && !slices.ContainsFunc(config.Cfg.Navigation.HintSkip, func(s string) bool { return strings.HasSuffix(label, "."+s+"()") }) &&
				!strings.HasSuffix(label, ".<clinit>()") {
				own = append(own, label)
			}
		} else if k == "M" && !strings.HasSuffix(label, ".<init>()") && !contains(found, label) {
			found = append(found, label)
		}
	}
	if len(found) == 0 {
		if len(own) > 0 {
			found = own[len(own)-1:]
		} else if len(via) > 0 {
			found = []string{g.label(via[0])}
		}
	}
	if len(found) > 2 {
		found = found[:2]
	}
	for i, x := range found {
		found[i] = strings.TrimSuffix(x, "()")
	}
	if len(found) == 2 && strings.Contains(found[1], ".") && rpartition(found[0]) == rpartition(found[1]) {
		found[1] = found[1][strings.LastIndex(found[1], ".")+1:]
	}
	return strings.Join(found, " > ")
}

// rpartition: the part before the last dot ("" without one).
func rpartition(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[:i]
	}
	return ""
}

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

// joined: where the way `other` (screen names, entry first) joins `main` for good: (the screens of
// `other` before the join, the join screen).
func joined(main, other []string) ([]string, string) {
	k := 0
	for k < min(len(main), len(other)) && main[len(main)-1-k] == other[len(other)-1-k] {
		k++
	}
	if k == 0 { // ways to different targets do not join
		return other, ""
	}
	return other[:len(other)-k], other[len(other)-k]
}

// ---------------------------------------------------------------- the walk

type navState struct {
	sid  int32
	host hostKey
}

type navStep struct {
	opens navState
	via   []int32
	hub   int32
	push  bool
}

type navRoot struct {
	sid int32
	st  navState
	how []navEntry
}

type navBranch struct {
	cost     int
	st       navState
	src, hub int32
}

type navHub struct {
	sid, hub int32
	below    []int32
}

type navResult struct {
	step       map[navState]*navStep // nil: a target
	stepOrder  []navState
	cost       map[navState]int
	depth      map[navState]int
	hubs       []navHub
	bases      []navHub
	roots      []navRoot
	branches   []navBranch // in the order their keys were first met
	targetsLen int
}

type heapItem struct {
	c    int
	name string
	sid  int32
	hstr string
	host hostKey
}

type navHeap []heapItem

func (h navHeap) Len() int { return len(h) }
func (h navHeap) Less(i, j int) bool {
	a, b := h[i], h[j]
	if a.c != b.c {
		return a.c < b.c
	}
	if a.name != b.name {
		return a.name < b.name
	}
	if a.sid != b.sid {
		return a.sid < b.sid
	}
	return a.hstr < b.hstr
}
func (h navHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *navHeap) Push(x any)   { *h = append(*h, x.(heapItem)) }
func (h *navHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

// hostStr: the host a way needs as text: a node by its id, a module by name, "" for none.
func (g *Graph) hostStr(h hostKey) string {
	if h.node >= 0 {
		return strconv.FormatInt(g.id(h.node), 10)
	}
	return h.mod
}

// navWalk: the Dijkstra walk of nav-path over states (screen, host), from the screen to the screens
// that open it, back to entry screens. launcherOnly: stop at the first launcher reached.
func (g *Graph) navWalk(targets []int32, maxDepth int, launcherOnly bool) *navResult {
	r := &navResult{step: map[navState]*navStep{}, cost: map[navState]int{}, depth: map[navState]int{},
		targetsLen: len(targets)}
	h := &navHeap{}
	for _, s := range targets {
		st := navState{s, noHost}
		if _, ok := r.step[st]; !ok {
			r.stepOrder = append(r.stepOrder, st)
		}
		r.step[st], r.cost[st], r.depth[st] = nil, 0, 0
		*h = append(*h, heapItem{0, g.screenName(s), s, "", noHost})
	}
	heap.Init(h)
	done, seen := map[navState]bool{}, map[int32]bool{}
	roots := map[int32]bool{}
	branchAt := map[int32]int{}
	for h.Len() > 0 {
		it := heap.Pop(h).(heapItem)
		c, sid, host := it.c, it.sid, it.host
		st := navState{sid, host}
		if done[st] {
			continue
		}
		done[st] = true
		if e := g.entriesOf(sid); len(e) > 0 && host.none() {
			if !roots[sid] {
				roots[sid] = true
				r.roots = append(r.roots, navRoot{sid, st, e})
			}
			if launcherOnly && e[0].kind == "launcher" {
				break
			}
			if r.depth[st] != 0 {
				continue
			}
		}
		if maxDepth >= 0 && r.depth[st] >= maxDepth {
			continue
		}
		first := !seen[sid]
		seen[sid] = true
		ways, found := g.openedBy(sid)
		if first {
			for _, b := range found {
				r.bases = append(r.bases, navHub{sid, b.Base, b.Below})
			}
			byHub := map[int32][]int32{}
			for _, w := range ways {
				if w.Hub >= 0 {
					byHub[w.Hub] = append(byHub[w.Hub], w.Src)
				}
			}
			for _, hub := range util.SortedKeys(byHub) {
				r.hubs = append(r.hubs, navHub{sid, hub, sortedInts(byHub[hub])})
			}
		}
		if len(ways) == 0 && !roots[sid] && host.none() {
			how := navEntry{"unknown", ""}
			if len(found) > 0 {
				how = navEntry{"base", g.label(found[0].Base)}
			} else if len(g.openers(sid)) > 0 {
				how = navEntry{"outside", ""}
			}
			roots[sid] = true
			r.roots = append(r.roots, navRoot{sid, st, []navEntry{how}})
		}
		for _, w := range ways {
			src, need := w.Src, w.host()
			if !host.none() { // a shared screen: opened in its host, or on the shared way
				switch {
				case src == host.node || host.mod != "" && g.module(src) == host.mod ||
					host.node >= 0 && containsInt(g.hosts(src), host.node):
				case g.screens[src][0].Kind == "activity" && host.node >= 0 && g.screenTops[host.node]:
					continue // another Activity cannot be the host the way needs
				case g.module(src) == g.module(sid):
					if need.none() {
						need = host
					}
				default:
					continue
				}
			}
			if need.mod != "" && need.mod == g.module(src) || need.node >= 0 && need.node == src {
				need = noHost
			}
			nst := navState{src, need}
			push := g.pushWay(w.Via)
			sc := c + config.StepOwn
			if w.Hub >= 0 {
				sc = c + config.StepShared
			}
			if push {
				sc += config.StepPush
			}
			key := sid
			if w.Hub >= 0 {
				key = w.Hub
			}
			if need.none() && !push && g.isLauncher(src) {
				if i, ok := branchAt[key]; !ok {
					branchAt[key] = len(r.branches)
					r.branches = append(r.branches, navBranch{sc, st, src, w.Hub})
				} else if sc < r.branches[i].cost {
					r.branches[i] = navBranch{sc, st, src, w.Hub}
				}
			}
			if old, ok := r.cost[nst]; done[nst] || ok && old <= sc {
				continue
			}
			r.cost[nst], r.depth[nst] = sc, r.depth[st]+1
			if _, ok := r.step[nst]; !ok {
				r.stepOrder = append(r.stepOrder, nst)
			}
			r.step[nst] = &navStep{st, w.Via, w.Hub, push}
			heap.Push(h, heapItem{sc, g.screenName(src), src, g.hostStr(need), need})
		}
	}
	kept := r.stepOrder[:0]
	for _, st := range r.stepOrder {
		if done[st] {
			kept = append(kept, st)
		} else {
			delete(r.step, st)
		}
	}
	r.stepOrder = kept
	sort.SliceStable(r.roots, func(i, j int) bool {
		a, b := r.roots[i], r.roots[j]
		if oa, ob := entryOrder[a.how[0].kind], entryOrder[b.how[0].kind]; oa != ob {
			return oa < ob
		}
		if ca, cb := r.cost[a.st], r.cost[b.st]; ca != cb {
			return ca < cb
		}
		return g.screenName(a.sid) < g.screenName(b.sid)
	})
	return r
}

func containsInt(s []int32, x int32) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

// chain: the states from `st` on to the target of the walk.
func (r *navResult) chain(st navState) []navState {
	out := []navState{st}
	for r.step[out[len(out)-1]] != nil {
		out = append(out, r.step[out[len(out)-1]].opens)
	}
	return out
}

func (g *Graph) chainNames(r *navResult, st navState) []string {
	var out []string
	for _, x := range r.chain(st) {
		out = append(out, g.screenName(x.sid))
	}
	return out
}

func (g *Graph) navArrows(r *navResult, st navState) string {
	out := []string{g.screenName(st.sid)}
	ch := r.chain(st)
	for _, x := range ch[:len(ch)-1] {
		arrow := "-> "
		if r.step[x].push {
			arrow = "~push~> "
		}
		out = append(out, arrow+g.screenName(r.step[x].opens.sid))
	}
	return strings.Join(out, " ")
}

type navOf struct {
	entry, detail, text string
	cost                int
	path                []string
}

// navOfScreen: the cheapest way to the screen from the launcher, else from its best other entry (impact
// --nav); nil for none.
func (g *Graph) navOfScreen(sid int32) *navOf {
	r := g.navWalk([]int32{sid}, -1, true)
	if len(r.roots) == 0 {
		return nil
	}
	root := r.roots[0]
	return &navOf{root.how[0].kind, root.how[0].detail, g.navArrows(r, root.st), r.cost[root.st], g.chainNames(r, root.st)}
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
	t := time.Now()
	r := g.navWalk(targets, o.depth, false)
	g.navReport(o, targets, r, time.Since(t))
	return exitOK
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
	limit := o.limit
	if limit < 0 {
		limit = 20
	}
	way := func(st navState) string {
		s := r.step[st]
		out := "own"
		if s.hub >= 0 {
			out = "shared"
		}
		if s.push {
			out = "push " + out
		}
		return out
	}
	// the cheapest way through each screen a launcher opens (not one coming back to the launcher)
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
	targetNames := make([]string, len(targets))
	for i, s := range targets {
		targetNames[i] = g.screenName(s)
	}

	if o.json {
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
			hosted := g.hosted(st.host)
			hs, _ := hosted.(string)
			o := append(g.navScreenJSON(st.sid), kv{"depth", r.depth[st]}, kv{"cost", r.cost[st]}, kv{"hosted_in", hosted},
				kv{"opens", g.screenName(s.opens.sid)}, kv{"way", way(st)}, kv{"shared", g.qualifiedOrNil(s.hub)},
				kv{"hint", g.stepHint(s.via)}, kv{"via", g.qualifiedList(s.via)})
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
			var how []any
			for _, e := range root.how {
				var d any
				if e.detail != "" {
					d = e.detail
				}
				how = append(how, obj{{"kind", e.kind}, {"detail", d}})
			}
			entries = append(entries, append(g.navScreenJSON(root.sid), kv{"entry", list(how)}))
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
		return
	}

	arrows := func(st navState) string { return g.navArrows(r, st) }
	if !o.verbose {
		if len(r.roots) == 0 {
			fmt.Printf("No way to %s found (as far as the code map sees).\n", strings.Join(targetNames, ", "))
			return
		}
		root := r.roots[0]
		st, how := root.st, root.how
		fmt.Printf("Cheapest way to %s (%s):\n", strings.Join(targetNames, ", "), entryText(how[0]))
		wayStates := r.chain(st)
		names := g.chainNames(r, st)
		width := 0
		for _, n := range names {
			width = max(width, utf8.RuneCountInString(n))
		}
		width += 3
		for i, x := range wayStates {
			arrow := ""
			if i > 0 {
				arrow = "-> "
				if r.step[wayStates[i-1]].push {
					arrow = "~push~> "
				}
			}
			hint := ""
			if r.step[x] != nil {
				hint = g.stepHint(r.step[x].via)
			}
			fmt.Println(strings.TrimRight(fmt.Sprintf("  %-*s %s", width+8, arrow+names[i], hint), " "))
		}
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
		for i, root := range r.roots {
			if k := root.how[0].kind; k == "deeplink" || k == "intent" {
				before, join := joined(names, g.chainNames(r, root.st))
				if n, c := len(before), r.cost[root.st]; best < 0 || n < bestN || n == bestN && c < bestC {
					best, bestN, bestC, bestJoin = i, n, c, join
				}
			}
		}
		if best >= 0 && len(names) > 1 {
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
		return
	}
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
	fmt.Println("  entry kinds: launcher, deeplink, intent (manifest); base: only a base class of many screens\n" +
		"  opens it; outside: opened from code that is not a screen (a service, push); unknown: the map\n" +
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
		fmt.Printf("  %-3d %-45s -> %s  [%s]\n", r.cost[st], name, g.screenName(s.opens.sid), way(st))
		if !o.noPaths {
			labels := make([]string, len(s.via))
			for i, n := range s.via {
				labels[i] = g.label(n)
			}
			fmt.Println("        " + strings.Join(labels, " <- "))
		}
	}
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
	fmt.Printf("(%.2fs)\n", elapsed.Seconds())
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

// gnode: a node of the diagram: a screen, a navigator (shared code), or an action: a method on screen
// `n` opening several screens.
type gnode struct {
	n      int32
	action string
	isAct  bool
}

type gedge struct {
	from, to           string
	label, hint, style string
}

type fwdWay struct {
	t, hub int32
	via    []int32
	need   hostKey
}

type gway struct {
	act   string
	t     int32
	hint  string
	style string
	cond  string
}

func (g *Graph) isDebug(sid int32) bool {
	p := g.filePath[g.fileOf(sid)]
	return slices.ContainsFunc(config.Cfg.Screens.DebugSourceSets, func(s string) bool { return strings.Contains(p, "/src/"+s+"/") })
}

// inHost: an arrow label with the host its way needs: `onNext · in feature/deposit`.
func inHost(label, host string) string {
	var parts []string
	if label != "" {
		parts = append(parts, label)
	}
	if host != "" {
		parts = append(parts, "in "+host)
	}
	return strings.Join(parts, " · ")
}

// shortAction: `CardsFeature.onFopRemindClick` -> `onFopRemindClick`; a use case's `X.invoke` -> `X`.
func shortAction(method string) string {
	cls, name := rpartition(method), method[strings.LastIndex(method, ".")+1:]
	if name == "invoke" && cls != "" {
		return cls[strings.LastIndex(cls, ".")+1:]
	}
	return name
}

// cmdGraph: what the screen opens, and what those open, up to --depth (1): forward over the
// opened_by rows. A way through shared code goes through a node of that code; one opening more than
// graphFanout screens ends in a "+N screens" node.
func (g *Graph) cmdGraph(o opts) int {
	roots := g.screensOf(o.pos[0], o.all)
	debug := true
	for _, r := range roots {
		debug = debug && g.isDebug(r)
	}
	forward := map[int32][]fwdWay{}
	hiddenDebug := 0
	for _, t := range g.openedTargets() {
		if g.isDebug(t) && !debug {
			hiddenDebug++
			continue
		}
		ways, _ := g.openedBy(t)
		for _, w := range ways {
			forward[w.Src] = append(forward[w.Src], fwdWay{t, w.Hub, w.Via, w.host()})
		}
	}
	rootModules := map[string]bool{}
	isRoot := map[int32]bool{}
	for _, r := range roots {
		rootModules[g.module(r)] = true
		isRoot[r] = true
	}
	context := func(t int32, parent map[hostKey]bool) map[hostKey]bool {
		hosts := g.hosts(t)
		var both []int32
		for _, h := range hosts {
			if parent[hostKey{h, ""}] {
				both = append(both, h)
			}
		}
		if len(both) > 0 {
			hosts = both
		}
		out := map[hostKey]bool{{t, ""}: true, {-1, g.module(t)}: true}
		for k := range parent {
			out[k] = true
		}
		for _, h := range hosts {
			out[hostKey{h, ""}], out[hostKey{-1, g.module(h)}] = true, true
		}
		return out
	}
	known := map[int32]map[hostKey]bool{}
	unsure := map[int32]bool{}
	for _, r := range roots {
		known[r] = context(r, map[hostKey]bool{})
		unsure[r] = true
	}
	dropped := 0
	runs := func(src int32, need hostKey) (bool, string) {
		if need.none() || known[src][need] {
			return true, ""
		}
		if unsure[src] {
			if need.node >= 0 {
				return true, g.label(need.node)
			}
			return true, need.mod
		}
		dropped++
		return false, ""
	}
	reach := func(src, t int32) {
		if _, ok := known[t]; !ok {
			known[t] = context(t, known[src])
			unsure[t] = unsure[src] && rootModules[g.module(t)]
		}
	}
	ids := map[gnode]string{}
	var order []gnode
	var edges []gedge
	more := map[int32][]int32{}
	node := func(n gnode) string {
		if id, ok := ids[n]; ok {
			return id
		}
		id := "n" + strconv.Itoa(len(ids)+1)
		ids[n] = id
		order = append(order, n)
		return id
	}
	action := func(hint string, hub int32) string {
		parts := []string{""}
		if hint != "" {
			parts = strings.Split(hint, " > ")
		}
		if hub >= 0 && len(parts) > 1 && strings.HasSuffix(strings.TrimRight(g.label(hub), "()"), parts[len(parts)-1]) {
			return parts[len(parts)-2]
		}
		return parts[len(parts)-1]
	}
	level := append([]int32(nil), roots...)
	seen, fanned := map[int32]bool{}, map[int32]bool{}
	for _, r := range roots {
		seen[r] = true
		node(gnode{n: r})
	}
	depth := o.depth
	if depth <= 0 {
		depth = 1
	}
	for range depth {
		var nxt []int32
		for _, src := range level {
			type tv struct {
				t    int32
				via  []int32
				cond string
			}
			byHub := map[int32][]tv{}
			var hubs []int32 // first-met order: ties of the sort below keep it
			fw := append([]fwdWay(nil), forward[src]...)
			sort.SliceStable(fw, func(i, j int) bool { return g.screenName(fw[i].t) < g.screenName(fw[j].t) })
			for _, w := range fw {
				if ok, cond := runs(src, w.need); ok {
					if _, has := byHub[w.hub]; !has {
						hubs = append(hubs, w.hub)
					}
					byHub[w.hub] = append(byHub[w.hub], tv{w.t, w.via, cond})
				}
			}
			sort.SliceStable(hubs, func(i, j int) bool {
				a, b := hubs[i], hubs[j]
				if (a >= 0) != (b >= 0) {
					return a < 0
				}
				if a < 0 {
					return false
				}
				return g.label(a) < g.label(b)
			})
			var ways []gway
			type fan struct {
				hub    int32
				opened []tv
			}
			var fans []fan
			for _, hub := range hubs {
				if hub < 0 {
					for _, x := range byHub[hub] {
						h := g.stepHint(x.via)
						ways = append(ways, gway{action(h, -1), x.t, h, "own", x.cond})
					}
					continue
				}
				h := g.stepHint(byHub[hub][0].via)
				ways = append(ways, gway{action(h, hub), hub, h, "shared", ""})
				if fanned[hub] {
					continue // the navigator's screens are drawn once
				}
				fanned[hub] = true
				fans = append(fans, fan{hub, byHub[hub]})
			}
			// one method opening several screens (onFopRemindClick choosing by state) is a node of its own
			var acts []string
			byAction := map[string][]gway{}
			for _, w := range ways {
				if _, ok := byAction[w.act]; !ok {
					acts = append(acts, w.act)
				}
				byAction[w.act] = append(byAction[w.act], w)
			}
			minName := func(a string) string {
				best := ""
				for i, w := range byAction[a] {
					var n string
					if _, ok := g.screens[w.t]; ok {
						n = g.screenName(w.t)
					} else {
						n = g.label(w.t)
					}
					if i == 0 || n < best {
						best = n
					}
				}
				return best
			}
			sort.SliceStable(acts, func(i, j int) bool { return minName(acts[i]) < minName(acts[j]) })
			for _, act := range acts {
				group := byAction[act]
				short := ""
				if act != "" {
					short = shortAction(act)
				}
				if len(group) > 1 && act != "" {
					a := node(gnode{src, act, true})
					edges = append(edges, gedge{node(gnode{n: src}), a, "", act, "act"})
					for _, w := range group {
						edges = append(edges, gedge{a, node(gnode{n: w.t}), inHost("", w.cond), w.hint, w.style})
					}
				} else {
					for _, w := range group {
						edges = append(edges, gedge{node(gnode{n: src}), node(gnode{n: w.t}), inHost(short, w.cond), w.hint, w.style})
					}
				}
				for _, w := range group {
					if w.style != "own" {
						continue
					}
					reach(src, w.t)
					if !seen[w.t] {
						nxt = append(nxt, w.t)
					}
					seen[w.t] = true
				}
			}
			for _, f := range fans {
				if len(f.opened) > config.GraphFanout {
					for _, x := range f.opened {
						more[f.hub] = append(more[f.hub], x.t)
					}
					continue
				}
				for _, x := range f.opened {
					edges = append(edges, gedge{node(gnode{n: f.hub}), node(gnode{n: x.t}), inHost("", x.cond), "", "fan"})
				}
				for _, x := range f.opened {
					reach(src, x.t)
				}
				for _, x := range f.opened {
					if !seen[x.t] {
						nxt = append(nxt, x.t)
					}
				}
				for _, x := range f.opened {
					seen[x.t] = true
				}
			}
		}
		level = nxt
	}
	cut := 0
	if len(edges) > config.GraphEdges {
		cut = len(edges) - config.GraphEdges
		edges = edges[:config.GraphEdges]
	}
	names := map[string]string{}
	for _, n := range order {
		var name string
		switch {
		case n.isAct:
			name = g.screenName(n.n) + ": " + n.action
		case g.screens[n.n] != nil:
			name = g.screenName(n.n)
		default:
			name = g.qualified(n.n)
		}
		names[ids[n]] = name
	}
	if o.json {
		var nodes, es []any
		for _, n := range order {
			kind := "shared"
			if n.isAct {
				kind = "action"
			} else if rows, ok := g.screens[n.n]; ok {
				kind = rows[0].Kind
			}
			nodes = append(nodes, obj{{"name", names[ids[n]]}, {"kind", kind}, {"module", g.module(n.n)}})
		}
		orNil := func(s string) any {
			if s == "" {
				return nil
			}
			return s
		}
		for _, e := range edges {
			var shared any
			if e.style == "shared" {
				shared = names[e.to]
			}
			es = append(es, obj{{"from", names[e.from]}, {"to", names[e.to]}, {"label", orNil(e.label)},
				{"hint", orNil(e.hint)}, {"shared", shared}})
		}
		moreObj := obj{}
		at := map[string]int{}
		for _, h := range util.SortedKeys(more) {
			ts := make([]string, len(more[h]))
			for i, t := range more[h] {
				ts[i] = g.screenName(t)
			}
			sort.Strings(ts)
			k := names[ids[gnode{n: h}]]
			if i, ok := at[k]; ok {
				moreObj[i].v = strs(ts)
				continue
			}
			at[k] = len(moreObj)
			moreObj = append(moreObj, kv{k, strs(ts)})
		}
		rootNames := make([]string, len(roots))
		for i, r := range roots {
			rootNames[i] = g.screenName(r)
		}
		g.printJSON("graph", obj{{"roots", strs(rootNames)}, {"nodes", list(nodes)}, {"edges", list(es)},
			{"more", moreObj}, {"edges_left_out", cut}, {"ways_of_other_hosts", dropped}, {"debug_left_out", hiddenDebug}})
		return exitOK
	}
	used := map[string]bool{}
	for _, e := range edges {
		used[e.from], used[e.to] = true, true
	}
	fmt.Println("flowchart LR")
	for _, n := range order {
		id := ids[n]
		if !used[id] && !(!n.isAct && isRoot[n.n]) {
			continue
		}
		var label, shape string
		switch {
		case n.isAct:
			label, shape = shortAction(n.action), `[["%s"]]`
		case isRoot[n.n]:
			label, shape = names[id], `(["%s"])`
		case g.screens[n.n] != nil:
			label, shape = names[id], `["%s"]`
		default:
			label, shape = g.label(n.n), `{{"%s"}}`
		}
		fmt.Printf("  %s"+shape+"\n", id, strings.ReplaceAll(label, `"`, "#quot;"))
	}
	for _, e := range edges {
		arrow := "-->"
		if e.style == "shared" {
			arrow = "-.->"
		}
		label := ""
		if e.label != "" {
			label = `|"` + e.label + `"|`
		}
		fmt.Printf("  %s %s%s %s\n", e.from, arrow, label, e.to)
	}
	hubs := util.SortedKeys(more)
	sort.SliceStable(hubs, func(i, j int) bool { return ids[gnode{n: hubs[i]}] < ids[gnode{n: hubs[j]}] })
	for _, h := range hubs {
		id := ids[gnode{n: h}]
		fmt.Printf("  %s --> %s_more[\"+%d screens\"]\n", id, id, len(more[h]))
	}
	if cut > 0 {
		fmt.Printf("  %%%% %d more edges left out (Mermaid draws up to %d)\n", cut, config.GraphEdges)
	}
	if dropped > 0 {
		fmt.Printf("  %%%% %d ways left out: they run only in a host the way here does not go through\n", dropped)
	}
	return exitOK
}
