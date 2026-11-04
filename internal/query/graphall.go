package query

// graph without a screen: every screen a user sees, every way one opens another, the navigators on
// the ways, the base classes whose code opens screens and the entries, from the opened_by rows as
// build wrote them (docs/decisions/0035).

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strconv"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/util"
)

// edgeWay: the way an edge of graph stands for: src opens t; ok false for an edge of no one way
// (a screen to its action node).
type edgeWay struct {
	src, t int32
	way    Way
	shared []SharedStep
	ok     bool
}

// callLine: the first line of src's code using dst, by any edge; 0: none known.
func (g *Graph) callLine(src, dst int32) int32 {
	o := &g.m.Out
	if src < 0 || len(g.m.OutLine) == 0 {
		return 0
	}
	lo, hi := int(o.Start[src]), int(o.Start[src+1])
	i := lo + sort.Search(hi-lo, func(k int) bool { return o.Node[lo+k] >= dst })
	line := int32(0)
	for ; i < hi && o.Node[i] == dst; i++ {
		if l := g.m.OutLine[i]; l != 0 && (line == 0 || l < line) {
			line = l
		}
	}
	return line
}

// lineOf: path:line in the file of n, "" without a line.
func (g *Graph) lineOf(n, line int32) string {
	p := g.filePath[g.fileOf(n)]
	if p == "" || line == 0 {
		return ""
	}
	g.show(n)
	return p + ":" + strconv.Itoa(int(line))
}

// navLocation: where the code opening t is: the call naming it in the opener (the first node of
// the way), else the first call toward the screen that has a line (an XML navigation action is named
// by generated Directions, without sources).
func (g *Graph) navLocation(t int32, via []int32) string {
	if len(via) == 0 {
		return ""
	}
	op := via[0]
	line := g.callLine(op, t)
	if line == 0 && g.kind(op) == "M" { // a fragment or a dialog: made by calling its constructor
		o := &g.m.Out
		for i := o.Start[op]; i < o.Start[op+1]; i++ {
			if l := g.m.OutLine[i]; l != 0 && g.topOf(o.Node[i]) == t && (line == 0 || l < line) {
				line = l
			}
		}
	}
	if at := g.lineOf(op, line); at != "" {
		return at
	}
	for k := 1; k < len(via); k++ {
		if at := g.lineOf(via[k], g.callLine(via[k], via[k-1])); at != "" {
			return at
		}
	}
	return ""
}

// sourceLocation: the first call on the way made from the source screen's side: the method nearest
// the screen that calls the next node toward the opener; the opener's when it is the screen's own.
func (g *Graph) sourceLocation(t int32, via []int32) string {
	for k := len(via) - 1; k >= 1; k-- {
		if g.kind(via[k]) != "M" {
			continue
		}
		if at := g.lineOf(via[k], g.callLine(via[k], via[k-1])); at != "" {
			return at
		}
	}
	return g.navLocation(t, via)
}

// wayBase: the base class whose inherited code the way runs through (one of BaseSubclasses or
// more subclasses, as impact's base), -1 for none.
func (g *Graph) wayBase(via []int32) int32 {
	for i := len(via) - 2; i >= 0; i-- {
		pt, ct := g.topOf(via[i]), g.topOf(via[i+1])
		if pt >= 0 && ct >= 0 && pt != ct && g.isSubtype(ct, pt) && g.subclassCount(pt) >= config.BaseSubclasses {
			return pt
		}
	}
	return -1
}

// wayVia: how a way opens its screen: through code of a base class the source inherits, through a
// navigator (a hub of opened_by), or by the source's own code; and the base.
func (g *Graph) wayVia(w Way) (string, int32) {
	if base := g.wayBase(w.Via); base >= 0 {
		return viaNames[viaBase], base
	}
	if w.Hub >= 0 {
		return viaNames[viaOpener], -1
	}
	return viaNames[viaDirect], -1
}

// edgeWayJSON: how the way of an edge opens its screen, and where in the code.
func (g *Graph) edgeWayJSON(e edgeWay) obj {
	if !e.ok {
		return obj{{"via", nil}, {"hub", nil}, {"base", nil}, {"shared", []any{}}, {"host", nil}, {"host_module", nil},
			{"location", nil}, {"source_location", nil}}
	}
	w := e.way
	via, base := g.wayVia(w)
	var host, hostModule any
	if w.HostNode >= 0 {
		host = g.nodeID(w.HostNode)
	}
	if w.HostModule != "" {
		hostModule = w.HostModule
	}
	orNil := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	shared := make([]any, len(e.shared))
	for i, st := range e.shared {
		shared[i] = obj{{"id", g.nodeID(st.Node)}, {"sources", int(st.Sources)}}
	}
	return obj{{"via", via}, {"hub", g.qualifiedOrNil(w.Hub)}, {"base", g.qualifiedOrNil(base)}, {"shared", shared},
		{"host", host},
		{"host_module", hostModule}, {"location", orNil(g.navLocation(e.t, w.Via))},
		{"source_location", orNil(g.sourceLocation(e.t, w.Via))}}
}

// nodeID: a node's id in graph: a screen's target, else the qualified name.
func (g *Graph) nodeID(n int32) string {
	if rows, ok := g.screens[n]; ok {
		return rows[0].Target
	}
	return g.qualified(n)
}

// screenNodeJSON: a screen as a node of graph, with the fields of screens.
func (g *Graph) screenNodeJSON(n int32) obj {
	var content []string
	for _, c := range g.drawn[n] {
		content = append(content, g.label(c))
	}
	slices.Sort(content)
	o := obj{{"id", g.nodeID(n)}, {"name", g.screenName(n)}, {"kind", g.screens[n][0].Kind}, {"module", g.module(n)},
		{"location", g.location(n)}}
	o = append(o, g.hostsJSON(g.hosts(n))...)
	return append(o, kv{"content", strs(content)}, kv{"previews", strs(g.previewNames(n))}, kv{"debug", g.isDebug(n)})
}

// otherNodeJSON: shared code or an action of graph, with the keys of a screen node.
func (g *Graph) otherNodeJSON(id, name, kind string, n int32) obj {
	return obj{{"id", id}, {"name", name}, {"kind", kind}, {"module", g.module(n)}, {"location", g.location(n)},
		{"host", nil}, {"hosts", []any{}}, {"content", []any{}}, {"previews", []any{}}, {"debug", g.isDebug(n)}}
}

type allEdge struct {
	from, to string
	e        edgeWay
}

// graphAll: graph without a screen: every way of the opened_by rows, no way dropped or merged.
func (g *Graph) graphAll(o opts) int {
	if o.in || o.to != "" || o.depth >= 0 || o.direction != "" {
		panic(util.UserError("graph without a SCREEN is the whole graph: --in, --to, --depth and --direction need a SCREEN"))
	}
	avoid, inside := g.screensMatching(o.avoid), g.inModules(o.modules)
	nodes := map[int32]bool{}
	var edges []allEdge
	hubTargets, hubSources := map[int32]map[int32]bool{}, map[int32]map[int32]bool{}
	baseOpens, baseBelow := map[int32]map[int32]bool{}, map[int32][]int32{}
	for _, t := range g.openedTargets() {
		if avoid[t] {
			continue
		}
		if inside(t) {
			nodes[t] = true
		}
		_, bases := g.openedBy(t)
		ways := g.graphWays(t)
		for _, b := range bases {
			var below []int32
			for _, s := range b.Below {
				if !avoid[s] && inside(s) {
					below = append(below, s)
				}
			}
			if len(below) == 0 && len(b.Below) > 0 {
				continue
			}
			if baseOpens[b.Base] == nil {
				baseOpens[b.Base] = map[int32]bool{}
			}
			baseOpens[b.Base][t], baseBelow[b.Base] = true, below
			nodes[t] = true
			for _, s := range below {
				nodes[s] = true
			}
		}
		for _, w := range ways {
			if avoid[w.Src] || !inside(w.Src) {
				continue
			}
			nodes[w.Src], nodes[t] = true, true
			edges = append(edges, allEdge{g.nodeID(w.Src), g.nodeID(t), edgeWay{w.Src, t, w.Way, w.shared, true}})
			if w.Hub >= 0 {
				if hubTargets[w.Hub] == nil {
					hubTargets[w.Hub], hubSources[w.Hub] = map[int32]bool{}, map[int32]bool{}
				}
				hubTargets[w.Hub][t], hubSources[w.Hub][w.Src] = true, true
			}
		}
	}
	slices.SortFunc(edges, func(a, b allEdge) int { return cmp.Or(cmp.Compare(a.from, b.from), cmp.Compare(a.to, b.to)) })
	ids := make([]int32, 0, len(nodes))
	for n := range nodes {
		ids = append(ids, n)
	}
	slices.SortFunc(ids, func(a, b int32) int { return cmp.Compare(g.nodeID(a), g.nodeID(b)) })
	type entry struct {
		screen, kind, detail, loc string
	}
	var entries []entry
	e := &g.m.Entry
	for i := range e.Node {
		n := g.m.Index(e.Node[i])
		if n < 0 || !nodes[n] {
			continue
		}
		loc := ""
		if p := g.filePath[e.File[i]]; p != "" {
			loc = p
			if e.Line[i] > 0 {
				loc += ":" + strconv.Itoa(int(e.Line[i]))
			}
		}
		entries = append(entries, entry{g.nodeID(n), e.Kind.At(i), e.Detail.At(i), loc})
	}
	slices.SortFunc(entries, func(a, b entry) int {
		return cmp.Or(cmp.Compare(a.screen, b.screen), cmp.Compare(a.kind, b.kind), cmp.Compare(a.detail, b.detail),
			cmp.Compare(a.loc, b.loc))
	})
	hubs := util.SortedKeys(hubTargets)
	slices.SortFunc(hubs, func(a, b int32) int { return cmp.Compare(g.qualified(a), g.qualified(b)) })
	bs := util.SortedKeys(baseOpens)
	slices.SortFunc(bs, func(a, b int32) int { return cmp.Compare(g.qualified(a), g.qualified(b)) })
	if !o.json {
		for _, x := range edges {
			via, _ := g.wayVia(x.e.way)
			label := ""
			if act := g.stepAction(g.stepHint(x.e.way.Via), x.e.way.Hub); act != "" {
				label = " " + shortAction(act)
			}
			fmt.Printf("%s -> %s  %s%s\n", x.from, x.to, via, label)
		}
		for _, b := range bs {
			fmt.Printf("%d screens of %s -> %d screens  base\n", len(baseBelow[b]), g.qualified(b), len(baseOpens[b]))
		}
		fmt.Printf("(%d screens, %d ways, %d navigators, %d bases, %d entries)\n", len(ids), len(edges), len(hubs),
			len(baseOpens), len(entries))
		return exitOK
	}
	nodeList := make([]any, len(ids))
	for i, n := range ids {
		nodeList[i] = g.screenNodeJSON(n)
	}
	edgeList := make([]any, len(edges))
	for i, x := range edges {
		hint := g.stepHint(x.e.way.Via)
		var label, h any
		if act := g.stepAction(hint, x.e.way.Hub); act != "" {
			label = shortAction(act)
		}
		if hint != "" {
			h = hint
		}
		edgeList[i] = append(obj{{"from", x.from}, {"to", x.to}, {"label", label}, {"hint", h}}, g.edgeWayJSON(x.e)...)
	}
	hubList := make([]any, len(hubs))
	for i, h := range hubs {
		hubList[i] = obj{{"id", g.qualified(h)}, {"name", g.label(h)}, {"module", g.module(h)}, {"location", g.location(h)},
			{"opens", len(hubTargets[h])}, {"opened_from", len(hubSources[h])}}
	}
	baseList := make([]any, len(bs))
	for i, b := range bs {
		opens, screens := []string{}, []string{}
		for t := range baseOpens[b] {
			opens = append(opens, g.nodeID(t))
		}
		for _, s := range baseBelow[b] {
			screens = append(screens, g.nodeID(s))
		}
		slices.Sort(opens)
		slices.Sort(screens)
		baseList[i] = obj{{"id", g.qualified(b)}, {"name", g.label(b)}, {"module", g.module(b)}, {"location", g.location(b)},
			{"screens", strs(screens)}, {"opens", strs(opens)}}
	}
	entryList := make([]any, len(entries))
	for i, x := range entries {
		var d, loc any
		if x.detail != "" {
			d = x.detail
		}
		if x.loc != "" {
			loc = x.loc
		}
		entryList[i] = obj{{"screen", x.screen}, {"kind", x.kind}, {"detail", d}, {"location", loc}}
	}
	g.printJSON("graph", obj{{"walk", "all"}, {"nodes", nodeList}, {"edges", edgeList}, {"hubs", hubList},
		{"bases", baseList}, {"entries", entryList}})
	return exitOK
}
