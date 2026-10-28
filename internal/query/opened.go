package query

// Who opens each screen: the walk from its openers to the screens they run in, done once by `build`
// for every screen a user sees.

import (
	"cmp"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/snapshot"
)

const hubRounds = 4 // walks past hubs

// Way: how a screen opens the target: hub -1 for code of its own; Via: the nodes from the opener to
// the screen; the host it needs: HostNode (an Activity hosting a route) or HostModule, or neither.
type Way struct {
	Hub        int32
	Via        []int32
	HostNode   int32
	HostModule string
}

// openers: methods that open the screen `sid`.
func (g *Graph) openers(sid int32) []int32 {
	kind := ""
	if rows, ok := g.screens[sid]; ok {
		kind = rows[0].Kind
	}
	var found []int32
	for _, u := range g.users(sid, snapshot.OPENS) {
		src := u.N
		var opens []int32
		if g.kind(src) == "R" {
			for _, d := range g.uses(src, snapshot.OPENS) {
				opens = append(opens, d.N)
			}
		}
		if len(opens) < 2 {
			found = append(found, src)
			continue
		}
		// an id of several navigation graphs: code of a module showing one of them opens its own
		mods := map[string]bool{}
		for _, d := range opens {
			mods[g.module(d)] = true
		}
		for _, r := range g.users(src, snapshot.RES, snapshot.XML_RES) {
			if m := g.module(r.N); !mods[m] || m == g.module(sid) {
				found = append(found, r.N)
			}
		}
	}
	if kind == "fragment" || kind == "dialog" {
		for _, m := range g.members(sid) {
			if g.name(m) == "<init>" {
				for _, u := range g.users(m, snapshot.CALL) {
					found = append(found, u.N)
				}
			}
		}
	}
	own := ""
	if kind != "route" {
		own = g.name(sid)
	}
	var out []int32
	seen := map[int32]bool{}
	for len(found) > 0 {
		src := found[len(found)-1]
		found = found[:len(found)-1]
		if seen[src] || g.diGlue[g.topOf(src)] {
			continue
		}
		seen[src] = true
		cls := g.classNameOf(src)
		if own != "" && (cls == own || strings.HasPrefix(cls, own+"$")) {
			if g.kind(src) == "M" && (g.flags(src)&accStatic != 0 || strings.HasSuffix(cls, "$Companion")) {
				for _, u := range g.users(src, snapshot.CALL, snapshot.LAMBDA, snapshot.INLINE) {
					found = append(found, u.N)
				}
			}
			continue
		}
		out = append(out, src)
	}
	slices.Sort(out)
	return out
}

// walkOpenedBy: ways = {screen: way}, bases = {class: [screens]}; capped:
// the pairs left out after pathLimit shortest ways.
// pushes: the components taking a push whose code reaches an opener without a screen on the way.
func (g *Graph) walkOpenedBy(sid int32) (ways map[int32]Way, bases map[int32][]int32, pushes []int32, capped int) {
	ways = map[int32]Way{}
	var seeds []Seed
	for _, src := range g.openers(sid) {
		seeds = append(seeds, Seed{src, modeMember})
	}
	if len(seeds) == 0 {
		return ways, map[int32][]int32{}, nil, 0
	}
	baseSet, skip := map[int32]bool{}, map[int32]bool{}
	for round := range hubRounds {
		alt := map[Key][]Parent{}
		w := g.walk(seeds, -1, nil, skip, baseSet, alt)
		if round == 0 { // later rounds skip the hubs, not the code a push runs
			for _, c := range w.ComponentOrder {
				if g.pushComponents[c] {
					pushes = append(pushes, c)
				}
			}
			slices.Sort(pushes)
		}
		var screens []int32
		inScreens := map[int32]bool{}
		for _, s := range w.ScreenOrder {
			if s != sid && screenKinds[g.screens[s][0].Kind] {
				screens = append(screens, s)
				inScreens[s] = true
			}
		}
		hubs, hubOrder := g.hubs(w, screens, inScreens)
		under := map[int32]int32{}
		for _, h := range hubOrder {
			for _, s := range hubs[h] {
				under[s] = h
			}
		}
		for _, s := range screens {
			_, isUnder := under[s]
			if _, has := ways[s]; isUnder && has {
				continue
			}
			// the screen opens it when one of the shortest ways runs there: that way explains it
			i := 0
			g.chains(w, alt, w.Screens[s], nil, nil, func(chain []Step) bool {
				if i == config.PathLimit {
					capped++
					return false
				}
				i++
				path := g.merged(chain)
				way := Way{Hub: -1, HostNode: g.routeHost(path, s)}
				if way.HostNode < 0 {
					module, ok := g.hostModule(path, s)
					if !ok {
						return true
					}
					way.HostModule = module
				}
				if h, ok := under[s]; ok {
					way.Hub = h
				}
				for _, st := range path {
					way.Via = append(way.Via, st.N)
				}
				ways[s] = way
				return false
			})
		}
		if len(hubs) == 0 {
			break
		}
		for h := range hubs {
			skip[h] = true
		}
	}
	bases = map[int32][]int32{}
	for b := range baseSet {
		top := true
		for o := range g.supSet(b) {
			top = top && !baseSet[o]
		}
		if top {
			bases[b] = g.screenSubclasses(b)
		}
	}
	return ways, bases, pushes, capped
}

// hubs: the lowest nodes of the walk tree with hubMin or more screens below, in first-met order.
func (g *Graph) hubs(w *Walk, screens []int32, inScreens map[int32]bool) (map[int32][]int32, []int32) {
	below := map[int32]map[int32]bool{}
	var order []int32
	children := map[int32]map[int32]bool{}
	for _, s := range screens {
		key, has := w.Screens[s], true
		child := int32(-1)
		for has {
			n := key.N
			if below[n] == nil {
				below[n] = map[int32]bool{}
				order = append(order, n)
			}
			below[n][s] = true
			if child >= 0 {
				if children[n] == nil {
					children[n] = map[int32]bool{}
				}
				children[n][child] = true
			}
			a := w.Aff[key]
			child, key, has = n, a.Parent, a.HasParent
		}
	}
	out := map[int32][]int32{}
	var outOrder []int32
	for _, n := range order {
		if len(below[n]) < config.HubMin || inScreens[n] {
			continue
		}
		lowest := true
		for c := range children[n] {
			lowest = lowest && len(below[c]) < config.HubMin
		}
		if lowest {
			var v []int32
			for s := range below[n] {
				v = append(v, s)
			}
			slices.Sort(v)
			out[n] = v
			outOrder = append(outOrder, n)
		}
	}
	return out, outOrder
}

// chains: every shortest way from a changed node to `key` (the first parent, then the other ones in
// the order the walk met them), to `emit` until it returns false; false when stopped.
func (g *Graph) chains(w *Walk, alt map[Key][]Parent, key Key, below []Key, suffix []Step, emit func([]Step) bool) bool {
	a := w.Aff[key]
	if !a.HasParent {
		return emit(append([]Step{{key.N, a.Why}}, suffix...))
	}
	below = append(below, key)
	for _, p := range append([]Parent{{a.Parent, a.Why}}, alt[key]...) {
		if slices.Contains(below, p.Key) {
			continue // parents at the same depth (a class's members) may loop
		}
		if !g.chains(w, alt, p.Key, below, append([]Step{{key.N, p.Why}}, suffix...), emit) {
			return false
		}
	}
	return true
}

// routeHost: a way through the content of a route several Activities register runs only when the
// Activity whose code is on the way (nearest to the route) hosts it: that Activity, or -1.
func (g *Graph) routeHost(path []Step, sid int32) int32 {
	if g.screens[sid][0].Kind != "route" || len(path) < 2 || path[len(path)-1].Why != "route content" {
		return -1
	}
	hosts := g.routeHostsOf(sid)
	if len(hosts) < 2 {
		return -1
	}
	for i := len(path) - 2; i >= 0; i-- {
		if top := g.topOf(path[i].N); hosts[top] {
			return top
		}
	}
	return -1
}

func (g *Graph) implementations(m int32) int {
	n := len(g.overriders(m))
	if g.flags(m)&snapshot.AccAbstract == 0 {
		n++
	}
	return n
}

// hostModule: ok false when the way cannot run in `sid` (a ViewModel's body
// of a method with several, and `sid` does not name the ViewModel); else the module whose screens
// must host `sid`, "" for none.
func (g *Graph) hostModule(path []Step, sid int32) (string, bool) {
	for i := len(path) - 1; i > 0; i-- {
		if path[i].Why != "call via supertype" {
			continue
		}
		impl := path[i-1].N
		several := false
		for _, sup := range g.overridden(impl) {
			several = several || g.implementations(sup) >= 2
		}
		if !several {
			continue
		}
		cls := g.topOf(impl)
		if g.isViewModel(cls) {
			if !g.screenNames(sid, cls) {
				return "", false
			}
			continue
		}
		if m := g.module(impl); m != g.module(sid) {
			return m, true
		}
		return "", true
	}
	return "", true
}

// screenNames: whether the code of screen `sid` names the class `cid` or a subclass of it.
func (g *Graph) screenNames(sid, cid int32) bool {
	if g.routeContents == nil {
		g.routeContents, g.routeScopes = map[int32]map[int32]bool{}, map[int32]map[int32]bool{}
		nested := map[string][][2]any{}
		for name, c := range g.classByName {
			if top, _, ok := strings.Cut(name, "$"); ok {
				nested[top] = append(nested[top], [2]any{name, c})
			}
		}
		for content, route := range g.routeOf {
			if g.routeContents[route] == nil {
				g.routeContents[route], g.routeScopes[route] = map[int32]bool{}, map[int32]bool{}
			}
			g.routeContents[route][g.topOf(content)] = true
			if g.kind(content) != "C" {
				g.routeScopes[route][-1] = true // a method: the class holding it decides
				continue
			}
			g.routeScopes[route][content] = true
			prefix := g.name(content) + "$"
			for _, x := range nested[g.name(g.topOf(content))] {
				if strings.HasPrefix(x[0].(string), prefix) {
					g.routeScopes[route][x[1].(int32)] = true
				}
			}
		}
	}
	g.subclassCount(cid) // builds children
	classes, stack := map[int32]bool{cid: true}, []int32{cid}
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, x := range g.children[c] {
			if !classes[x] {
				classes[x] = true
				stack = append(stack, x)
			}
		}
	}
	scope := g.routeScopes[sid]
	if len(scope) > 0 && !scope[-1] {
		for c := range classes {
			for _, u := range edgesOf(&g.m.In, c, nil) {
				if u.Kind != snapshot.INJECT && scope[g.owner(u.N)] {
					return true
				}
			}
		}
		return false
	}
	tops := map[int32]bool{sid: true}
	for t := range g.routeContents[sid] {
		tops[t] = true
	}
	for c := range classes {
		for _, u := range edgesOf(&g.m.In, c, nil) {
			if t := g.m.Node.TopIx[u.N]; u.Kind != snapshot.INJECT && t >= 0 && tops[t] {
				return true
			}
		}
	}
	return false
}

func (g *Graph) screenSubclasses(c int32) []int32 {
	var out []int32
	for s := range g.screenTops {
		if g.supSet(s)[c] {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}

// openedTargets: the screens build walks: the ones a user sees.
func (g *Graph) openedTargets() []int32 {
	var out []int32
	for s, rows := range g.screens {
		if screenKinds[rows[0].Kind] {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}

// precomputeOpenedBy walks opened_by for every target screen, a Graph per worker (its caches are not
// shared), into the map's OpenedBy and OpenedBase columns. Returns (screens, capped pairs).
func PrecomputeOpenedBy(m *snapshot.Map, root string, jobs int) (int, int) {
	targets := NewGraph(m, root).openedTargets()
	type result struct {
		ways   map[int32]Way
		bases  map[int32][]int32
		pushes []int32
		capped int
	}
	results := make([]result, len(targets))
	var next atomic.Int64
	var wg sync.WaitGroup
	for range max(jobs, 1) {
		wg.Go(func() {
			g := NewGraph(m, root)
			for {
				i := int(next.Add(1)) - 1
				if i >= len(targets) {
					return
				}
				w, b, p, c := g.walkOpenedBy(targets[i])
				results[i] = result{w, b, p, c}
			}
		})
	}
	wg.Wait()
	o, ob := &m.OpenedBy, &m.OpenedBase
	*o, *ob = snapshot.OpenedRows{}, snapshot.BaseRows{}
	o.Via.Start, ob.Below.Start = []int32{0}, []int32{0}
	capped := 0
	for i, s := range targets {
		r := results[i]
		capped += r.capped
		srcs := make([]int32, 0, len(r.ways))
		for src := range r.ways {
			srcs = append(srcs, src)
		}
		slices.Sort(srcs)
		for _, src := range srcs {
			w := r.ways[src]
			o.Screen, o.Src, o.Hub, o.HostNode = append(o.Screen, s), append(o.Src, src), append(o.Hub, w.Hub), append(o.HostNode, w.HostNode)
			o.HostModule.Add(w.HostModule)
			o.Via.Node = append(o.Via.Node, w.Via...)
			o.Via.Start = append(o.Via.Start, int32(len(o.Via.Node)))
		}
		bs := make([]int32, 0, len(r.bases))
		for b := range r.bases {
			bs = append(bs, b)
		}
		slices.Sort(bs)
		for _, b := range bs {
			ob.Screen, ob.Base = append(ob.Screen, s), append(ob.Base, b)
			ob.Below.Node = append(ob.Below.Node, r.bases[b]...)
			ob.Below.Start = append(ob.Below.Start, int32(len(ob.Below.Node)))
		}
	}
	addPushEntries(m, targets, func(i int) []int32 { return results[i].pushes })
	return len(targets), capped
}

// addPushEntries: a push entry of each target screen for each component taking a push that opens
// it, the component's name as its detail; the entries stay sorted as build wrote them.
func addPushEntries(m *snapshot.Map, targets []int32, pushes func(int) []int32) {
	g := NewGraph(m, "")
	type row struct {
		node         int64
		kind, detail string
		file, line   int32
	}
	e := &m.Entry
	var rows []row
	for i := range e.Node {
		rows = append(rows, row{e.Node[i], e.Kind.At(i), e.Detail.At(i), e.File[i], e.Line[i]})
	}
	n := len(rows)
	for i, s := range targets {
		for _, c := range pushes(i) {
			rows = append(rows, row{m.Node.ID[s], "push", g.label(c), g.fileOf(c), g.m.Node.LMin[c]})
		}
	}
	if len(rows) == n {
		return
	}
	slices.SortFunc(rows, func(a, b row) int {
		return cmp.Or(cmp.Compare(a.node, b.node), cmp.Compare(a.kind, b.kind), cmp.Compare(a.detail, b.detail),
			cmp.Compare(a.file, b.file), cmp.Compare(a.line, b.line))
	})
	rows = slices.Compact(rows)
	*e = snapshot.Map{}.Entry
	for _, r := range rows {
		e.Node = append(e.Node, r.node)
		e.Kind.Add(r.kind)
		e.Detail.Add(r.detail)
		e.File = append(e.File, r.file)
		e.Line = append(e.Line, r.line)
	}
}

// openedFrom: the screens that open `sid` (through shared code too), from the build's walk.
func (g *Graph) openedFrom(sid int32) []int32 {
	o := &g.m.OpenedBy
	lo := sort.Search(len(o.Screen), func(i int) bool { return o.Screen[i] >= sid })
	var out []int32
	for i := lo; i < len(o.Screen) && o.Screen[i] == sid; i++ {
		out = append(out, o.Src[i])
	}
	return out
}
