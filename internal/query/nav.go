package query

// The walk of nav-path and impact --nav over the opened_by rows build wrote, back from a screen to
// the entries; the commands are in navpath.go and graphcmd.go.

import (
	"container/heap"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/util"
)

var entryOrder = map[string]int{"launcher": 0, "deeplink": 1, "intent": 2, "base": 3, "outside": 4, "unknown": 5}

// mermaidDirections: how a Mermaid flowchart runs, what graph --direction takes (TD is TB).
var mermaidDirections = []string{"LR", "RL", "TB", "TD", "BT"}

// entryKinds: the entries of the manifest, what nav-path --entry takes.
var entryKinds = []string{"launcher", "deeplink", "intent"}

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
		if g.pushes[n] == 0 { // asked on every step of every walk
			g.pushes[n] = 1
			if g.kind(n) == "M" && slices.ContainsFunc(config.Cfg.NavPath.PushMarkers, func(s string) bool { return strings.Contains(g.name(n), s) }) {
				g.pushes[n] = 2
			}
		}
		if g.pushes[n] == 2 {
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
	known      *navState // the state in no host of a screen lim.reached has, where the walk stopped
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

// navLimits: where a walk ends and what it keeps off. from: the screens it ends at, in any host
// (nil: the entries of the manifest, of the kind `entry` when set); avoid: screens no way goes through.
type navLimits struct {
	maxDepth     int
	launcherOnly bool           // stop at the first launcher reached
	anyEntry     bool           // stop at the first entry or base reached: whether, not how
	reached      map[int32]bool // with anyEntry: screens known reached, whose state in no host ends the walk too
	from, avoid  map[int32]bool
	entry        string
	direct       bool // the first step reaches `from`
}

// restricted: the walk ends only at the screens asked for, so a screen nothing opens is no answer.
func (l navLimits) restricted() bool { return l.from != nil || l.entry != "" }

// rootsOf: how the walk may end at the screen in `host`.
func (g *Graph) rootsOf(sid int32, host hostKey, lim navLimits) []navEntry {
	if lim.from != nil {
		if lim.from[sid] {
			return []navEntry{{"from", ""}}
		}
		return nil
	}
	if !host.none() {
		return nil
	}
	e := g.entriesOf(sid)
	if lim.entry == "" {
		return e
	}
	var kept []navEntry
	for _, x := range e {
		if x.kind == lim.entry {
			kept = append(kept, x)
		}
	}
	return kept
}

func statesOf(sids []int32) []navState {
	out := make([]navState, len(sids))
	for i, s := range sids {
		out[i] = navState{s, noHost}
	}
	return out
}

// navWalker is the state of one navWalk.
type navWalker struct {
	g        *Graph
	lim      navLimits
	r        *navResult
	h        *navHeap
	done     map[navState]bool
	seen     map[int32]bool
	roots    map[int32]bool
	branchAt map[int32]int
}

// navWalk: the Dijkstra walk of nav-path over states (screen, host), from the states `starts` to the
// screens that open them, back to entry screens.
func (g *Graph) navWalk(starts []navState, lim navLimits) *navResult {
	r := &navResult{step: map[navState]*navStep{}, cost: map[navState]int{}, depth: map[navState]int{},
		targetsLen: len(starts)}
	h := &navHeap{}
	for _, st := range starts {
		if _, ok := r.step[st]; !ok {
			r.stepOrder = append(r.stepOrder, st)
		}
		r.step[st], r.cost[st], r.depth[st] = nil, 0, 0
		*h = append(*h, heapItem{0, g.screenName(st.sid), st.sid, g.hostStr(st.host), st.host})
	}
	heap.Init(h)
	nw := &navWalker{g: g, lim: lim, r: r, h: h, done: map[navState]bool{}, seen: map[int32]bool{},
		roots: map[int32]bool{}, branchAt: map[int32]int{}}
	for h.Len() > 0 {
		if nw.visit(heap.Pop(h).(heapItem)) {
			break
		}
	}
	nw.finish()
	return r
}

// visit: the ways to a state popped from the heap; true: the walk is over.
func (nw *navWalker) visit(it heapItem) bool {
	g, r, lim := nw.g, nw.r, nw.lim
	sid, host := it.sid, it.host
	st := navState{sid, host}
	if nw.done[st] {
		return false
	}
	nw.done[st] = true
	if lim.reached[sid] && host.none() {
		r.known = &st
		return true
	}
	if e := g.rootsOf(sid, host, lim); len(e) > 0 {
		if !nw.roots[sid] {
			nw.roots[sid] = true
			r.roots = append(r.roots, navRoot{sid, st, e})
		}
		if lim.launcherOnly && e[0].kind == "launcher" ||
			lim.anyEntry && slices.ContainsFunc(e, func(x navEntry) bool { return slices.Contains(entryKinds, x.kind) }) {
			return true
		}
		if r.depth[st] != 0 {
			return false
		}
	}
	if lim.maxDepth >= 0 && r.depth[st] >= lim.maxDepth {
		return false
	}
	first := !nw.seen[sid]
	nw.seen[sid] = true
	ways, found := g.openedBy(sid)
	if first {
		nw.hubsOf(sid, ways, found)
	}
	if len(ways) == 0 && !nw.roots[sid] && host.none() && !lim.restricted() {
		how := navEntry{"unknown", ""}
		if len(found) > 0 {
			how = navEntry{"base", g.label(found[0].Base)}
		} else if len(g.openers(sid)) > 0 {
			how = navEntry{"outside", ""}
		}
		nw.roots[sid] = true
		r.roots = append(r.roots, navRoot{sid, st, []navEntry{how}})
		if lim.anyEntry && how.kind == "base" {
			return true
		}
	}
	for _, w := range ways {
		nw.way(st, it.c, w)
	}
	return false
}

// hubsOf: the bases and hubs a screen is opened through, listed once.
func (nw *navWalker) hubsOf(sid int32, ways []srcWay, found []baseBelow) {
	r := nw.r
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

// way: the state of the screen opening `st` by the way `w`, pushed when it costs less than known.
func (nw *navWalker) way(st navState, c int, w srcWay) {
	g, r, lim := nw.g, nw.r, nw.lim
	sid, host := st.sid, st.host
	src, need := w.Src, w.host()
	if lim.avoid[src] || lim.direct && r.depth[st] == 0 && !lim.from[src] {
		return
	}
	if !host.none() { // a shared screen: opened in its host, or on the shared way
		switch {
		case src == host.node || host.mod != "" && g.module(src) == host.mod ||
			host.node >= 0 && containsInt(g.hosts(src), host.node):
		case g.screens[src][0].Kind == "activity" && host.node >= 0 && g.screenTops[host.node]:
			return // another Activity cannot be the host the way needs
		case g.module(src) == g.module(sid):
			if need.none() {
				need = host
			}
		default:
			return
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
	if need.none() && !push && g.isLauncher(src) && !lim.restricted() {
		if i, ok := nw.branchAt[key]; !ok {
			nw.branchAt[key] = len(r.branches)
			r.branches = append(r.branches, navBranch{sc, st, src, w.Hub})
		} else if sc < r.branches[i].cost {
			r.branches[i] = navBranch{sc, st, src, w.Hub}
		}
	}
	if old, ok := r.cost[nst]; nw.done[nst] || ok && old <= sc {
		return
	}
	r.cost[nst], r.depth[nst] = sc, r.depth[st]+1
	if _, ok := r.step[nst]; !ok {
		r.stepOrder = append(r.stepOrder, nst)
	}
	r.step[nst] = &navStep{st, w.Via, w.Hub, push}
	heap.Push(nw.h, heapItem{sc, g.screenName(src), src, g.hostStr(need), need})
}

// finish: only the states the walk reached, the roots by kind, cost and name.
func (nw *navWalker) finish() {
	g, r := nw.g, nw.r
	kept := r.stepOrder[:0]
	for _, st := range r.stepOrder {
		if nw.done[st] {
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
