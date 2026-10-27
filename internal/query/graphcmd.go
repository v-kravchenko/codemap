package query

// graph: what a screen opens, or with --in what opens it, as a diagram over the opened_by rows.

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/util"
)

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
	if o.in {
		if o.to != "" {
			panic(util.UserError("argument --to: not allowed with argument --in"))
		}
		return g.graphIn(o, roots)
	}
	forward, hiddenDebug := g.forwardWays(roots, g.screensMatching(o.avoid))
	d := &gdraw{roots: roots, isRoot: map[int32]bool{}, ids: map[gnode]string{}, more: map[int32][]int32{},
		hiddenDebug: hiddenDebug, depth: o.depth}
	gw := &graphOut{g: g, d: d, forward: forward, inside: g.inModules(o.modules), rootModules: map[string]bool{},
		known: map[int32]map[hostKey]bool{}, unsure: map[int32]bool{}, seen: map[int32]bool{}, fanned: map[int32]bool{}}
	if o.to != "" {
		gw.toward(g.screensOf(o.to, o.all))
	}
	if d.depth <= 0 {
		d.depth = 1
	}
	for _, r := range roots {
		gw.rootModules[g.module(r)] = true
		d.isRoot[r] = true
	}
	for _, r := range roots {
		gw.known[r] = gw.context(r, map[hostKey]bool{})
		gw.unsure[r] = true
	}
	level := append([]int32(nil), roots...)
	for _, r := range roots {
		gw.seen[r] = true
		gw.node(gnode{n: r})
	}
	for step := range d.depth {
		var nxt []int32
		for _, src := range level {
			nxt = append(nxt, gw.expand(src, step)...)
		}
		level = nxt
	}
	g.printGraph(o, d)
	return exitOK
}

// forwardWays: the ways each screen opens others, without those to debug screens (unless a root is
// one) and to avoided ones.
func (g *Graph) forwardWays(roots []int32, avoid map[int32]bool) (map[int32][]fwdWay, int) {
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
		if avoid[t] && !slices.Contains(roots, t) {
			continue
		}
		ways, _ := g.openedBy(t)
		for _, w := range ways {
			forward[w.Src] = append(forward[w.Src], fwdWay{t, w.Hub, w.Via, w.host()})
		}
	}
	return forward, hiddenDebug
}

// graphOut is the state of drawing graph forward from its roots.
type graphOut struct {
	g           *Graph
	d           *gdraw
	forward     map[int32][]fwdWay
	toDist      map[int32]int // --to: the fewest steps from each screen to a target
	inside      func(int32) bool
	rootModules map[string]bool
	known       map[int32]map[hostKey]bool // the hosts a screen is known to run in
	unsure      map[int32]bool             // reached in the roots' modules only: its hosts may be any
	seen        map[int32]bool
	fanned      map[int32]bool
}

// toward: --to: a way is drawn only when it gets to a target in time.
func (gw *graphOut) toward(to []int32) {
	d := gw.d
	d.to = to
	gw.toDist = gw.g.stepsTo(gw.forward, to)
	fewest := -1
	for _, r := range d.roots {
		if n, ok := gw.toDist[r]; ok && (fewest < 0 || n < fewest) {
			fewest = n
		}
	}
	if d.depth <= 0 && fewest >= 0 {
		d.depth = fewest + 2 // the shortest ways and a detour or two
	}
	d.noWay = fewest < 0 || d.depth > 0 && fewest > d.depth
}

func (gw *graphOut) context(t int32, parent map[hostKey]bool) map[hostKey]bool {
	g := gw.g
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

// runs: whether a way needing the host `need` runs from `src`, and the host it is shown under when unsure.
func (gw *graphOut) runs(src int32, need hostKey) (bool, string) {
	if need.none() || gw.known[src][need] {
		return true, ""
	}
	if gw.unsure[src] {
		if need.node >= 0 {
			return true, gw.g.label(need.node)
		}
		return true, need.mod
	}
	gw.d.dropped++
	return false, ""
}

func (gw *graphOut) reach(src, t int32) {
	if _, ok := gw.known[t]; !ok {
		gw.known[t] = gw.context(t, gw.known[src])
		gw.unsure[t] = gw.unsure[src] && gw.rootModules[gw.g.module(t)]
	}
}

func (gw *graphOut) node(n gnode) string {
	d := gw.d
	if id, ok := d.ids[n]; ok {
		return id
	}
	id := "n" + strconv.Itoa(len(d.ids)+1)
	d.ids[n] = id
	d.order = append(d.order, n)
	return id
}

// graphTarget: a screen a way opens, the code on the way and the host it needs, shown when unsure.
type graphTarget struct {
	t    int32
	via  []int32
	cond string
}

type graphFan struct {
	hub    int32
	opened []graphTarget
}

// expand: the edges from `src` at step `step`; returns the screens to expand next.
func (gw *graphOut) expand(src int32, step int) []int32 {
	g := gw.g
	byHub := map[int32][]graphTarget{}
	var hubs []int32 // first-met order: ties of the sort below keep it
	fw := append([]fwdWay(nil), gw.forward[src]...)
	sort.SliceStable(fw, func(i, j int) bool { return g.screenName(fw[i].t) < g.screenName(fw[j].t) })
	for _, w := range fw {
		if n, ok := gw.toDist[w.t]; gw.toDist != nil && (!ok || n > gw.d.depth-step-1 || gw.d.isRoot[w.t]) { // back to the start: a loop
			continue
		}
		if ok, cond := gw.runs(src, w.need); ok {
			if _, has := byHub[w.hub]; !has {
				hubs = append(hubs, w.hub)
			}
			byHub[w.hub] = append(byHub[w.hub], graphTarget{w.t, w.via, cond})
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
	var fans []graphFan
	for _, hub := range hubs {
		if hub < 0 {
			for _, x := range byHub[hub] {
				h := g.stepHint(x.via)
				ways = append(ways, gway{g.stepAction(h, -1), x.t, h, "own", x.cond})
			}
			continue
		}
		h := g.stepHint(byHub[hub][0].via)
		ways = append(ways, gway{g.stepAction(h, hub), hub, h, "shared", ""})
		if gw.fanned[hub] {
			continue // the navigator's screens are drawn once
		}
		gw.fanned[hub] = true
		fans = append(fans, graphFan{hub, byHub[hub]})
	}
	nxt := gw.drawWays(src, ways)
	for _, f := range fans {
		nxt = append(nxt, gw.drawFan(src, f)...)
	}
	return nxt
}

// drawWays: the edges of the ways from `src`; one method opening several screens (onFopRemindClick
// choosing by state) is a node of its own.
func (gw *graphOut) drawWays(src int32, ways []gway) []int32 {
	g, d := gw.g, gw.d
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
	var nxt []int32
	for _, act := range acts {
		group := byAction[act]
		short := ""
		if act != "" {
			short = shortAction(act)
		}
		if len(group) > 1 && act != "" {
			a := gw.node(gnode{src, act, true})
			d.edges = append(d.edges, gedge{gw.node(gnode{n: src}), a, "", act, "act"})
			for _, w := range group {
				d.edges = append(d.edges, gedge{a, gw.node(gnode{n: w.t}), inHost("", w.cond), w.hint, w.style})
			}
		} else {
			for _, w := range group {
				d.edges = append(d.edges, gedge{gw.node(gnode{n: src}), gw.node(gnode{n: w.t}), inHost(short, w.cond), w.hint, w.style})
			}
		}
		for _, w := range group {
			if w.style != "own" {
				continue
			}
			gw.reach(src, w.t)
			if !gw.seen[w.t] && gw.inside(w.t) {
				nxt = append(nxt, w.t)
			}
			gw.seen[w.t] = true
		}
	}
	return nxt
}

// drawFan: the screens shared code opens, or a "+N screens" node for more than graphFanout.
func (gw *graphOut) drawFan(src int32, f graphFan) []int32 {
	d := gw.d
	if len(f.opened) > config.GraphFanout {
		for _, x := range f.opened {
			d.more[f.hub] = append(d.more[f.hub], x.t)
		}
		return nil
	}
	for _, x := range f.opened {
		d.edges = append(d.edges, gedge{gw.node(gnode{n: f.hub}), gw.node(gnode{n: x.t}), inHost("", x.cond), "", "fan"})
	}
	for _, x := range f.opened {
		gw.reach(src, x.t)
	}
	var nxt []int32
	for _, x := range f.opened {
		if !gw.seen[x.t] && gw.inside(x.t) {
			nxt = append(nxt, x.t)
		}
	}
	for _, x := range f.opened {
		gw.seen[x.t] = true
	}
	return nxt
}

// stepAction: the method on the screen a step starts from, from its hint; for a step through shared
// code, the one calling that code.
func (g *Graph) stepAction(hint string, hub int32) string {
	parts := []string{""}
	if hint != "" {
		parts = strings.Split(hint, " > ")
	}
	if hub >= 0 && len(parts) > 1 && strings.HasSuffix(strings.TrimRight(g.label(hub), "()"), parts[len(parts)-1]) {
		return parts[len(parts)-2]
	}
	return parts[len(parts)-1]
}

// graphIn: graph --in: what opens the roots, and what opens those, up to --depth (1): back over the
// opened_by rows. Shared code stands between the screens calling it and the screen it opens; with
// more than graphFanout callers it starts at a "+N screens" node. Hosts are not followed: every
// way that opens a screen is drawn.
func (g *Graph) graphIn(o opts, roots []int32) int {
	avoid, inside := g.screensMatching(o.avoid), g.inModules(o.modules)
	debug := true
	for _, r := range roots {
		debug = debug && g.isDebug(r)
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
	isRoot, seen := map[int32]bool{}, map[int32]bool{}
	for _, r := range roots {
		isRoot[r], seen[r] = true, true
		node(gnode{n: r})
	}
	depth := max(o.depth, 1)
	hiddenDebug := 0
	drawn := map[[2]int32]bool{} // from, to
	edge := func(from, to int32, label, hint, style string) {
		if !drawn[[2]int32{from, to}] {
			drawn[[2]int32{from, to}] = true
			edges = append(edges, gedge{node(gnode{n: from}), node(gnode{n: to}), label, hint, style})
		}
	}
	level := append([]int32(nil), roots...)
	for range depth {
		var nxt []int32
		reach := func(src int32) {
			if !seen[src] && inside(src) {
				nxt = append(nxt, src)
			}
			seen[src] = true
		}
		for _, t := range level {
			ways, _ := g.openedBy(t)
			ways = append([]srcWay(nil), ways...)
			sort.SliceStable(ways, func(i, j int) bool { return g.screenName(ways[i].Src) < g.screenName(ways[j].Src) })
			byHub := map[int32][]srcWay{}
			var hubs []int32
			for _, w := range ways {
				if avoid[w.Src] {
					continue
				}
				if g.isDebug(w.Src) && !debug {
					hiddenDebug++
					continue
				}
				if w.Hub >= 0 {
					if _, ok := byHub[w.Hub]; !ok {
						hubs = append(hubs, w.Hub)
					}
					byHub[w.Hub] = append(byHub[w.Hub], w)
					continue
				}
				hint := g.stepHint(w.Via)
				label := ""
				if act := g.stepAction(hint, -1); act != "" {
					label = shortAction(act)
				}
				edge(w.Src, t, label, hint, "own")
				reach(w.Src)
			}
			sort.SliceStable(hubs, func(i, j int) bool { return g.label(hubs[i]) < g.label(hubs[j]) })
			for _, hub := range hubs {
				edge(hub, t, "", "", "fan")
				var srcs []int32
				for _, w := range byHub[hub] {
					if !containsInt(srcs, w.Src) {
						srcs = append(srcs, w.Src)
					}
				}
				if len(srcs) > config.GraphFanout {
					for _, src := range srcs {
						if !containsInt(more[hub], src) {
							more[hub] = append(more[hub], src)
						}
					}
					continue
				}
				for _, w := range byHub[hub] {
					hint := g.stepHint(w.Via)
					label := ""
					if act := g.stepAction(hint, hub); act != "" {
						label = shortAction(act)
					}
					edge(w.Src, hub, label, hint, "shared")
					reach(w.Src)
				}
			}
		}
		level = nxt
	}
	g.printGraph(o, &gdraw{roots: roots, isRoot: isRoot, order: order, ids: ids, edges: edges, more: more,
		hiddenDebug: hiddenDebug, in: true, depth: depth})
	return exitOK
}

// gdraw: a diagram of graph: its nodes in the order met, edges, the screens behind collapsed shared
// code, and what was left out.
type gdraw struct {
	roots, to            []int32
	isRoot               map[int32]bool
	order                []gnode
	ids                  map[gnode]string
	edges                []gedge
	more                 map[int32][]int32
	dropped, hiddenDebug int
	in, noWay            bool // in: who opens the roots, not what they open
	depth                int
}

func (g *Graph) printGraph(o opts, d *gdraw) {
	roots, isRoot, order, ids, edges, more, dropped, hiddenDebug := d.roots, d.isRoot, d.order, d.ids, d.edges, d.more,
		d.dropped, d.hiddenDebug
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
		walk := "out"
		if d.in {
			walk = "in"
		}
		toNames := make([]string, len(d.to))
		for i, t := range d.to {
			toNames[i] = g.screenName(t)
		}
		g.printJSON("graph", obj{{"roots", strs(rootNames)}, {"walk", walk}, {"to", strs(toNames)},
			{"nodes", list(nodes)}, {"edges", list(es)}, {"more", moreObj}, {"edges_left_out", cut},
			{"ways_of_other_hosts", dropped}, {"debug_left_out", hiddenDebug}})
		return
	}
	used := map[string]bool{}
	for _, e := range edges {
		used[e.from], used[e.to] = true, true
	}
	direction := o.direction
	if direction == "" {
		direction = "LR"
	}
	fmt.Println("flowchart " + direction)
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
		if d.in {
			fmt.Printf("  %s_more[\"+%d screens\"] -.-> %s\n", id, len(more[h]), id)
		} else {
			fmt.Printf("  %s --> %s_more[\"+%d screens\"]\n", id, id, len(more[h]))
		}
	}
	if cut > 0 {
		fmt.Printf("  %%%% %d more edges left out (Mermaid draws up to %d)\n", cut, config.GraphEdges)
	}
	if dropped > 0 {
		fmt.Printf("  %%%% %d ways left out: they run only in a host the way here does not go through\n", dropped)
	}
	if d.noWay {
		names := make([]string, len(d.to))
		for i, t := range d.to {
			names[i] = g.screenName(t)
		}
		steps := "steps"
		if d.depth == 1 {
			steps = "step"
		}
		fmt.Printf("  %%%% no way to %s within %d %s\n", strings.Join(names, ", "), d.depth, steps)
	}
}
