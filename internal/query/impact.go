package query

// impact: the walk from changed nodes to everything that may change.

import (
	"encoding/binary"
	"slices"
	"strings"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/snapshot"
)

const (
	modeMember byte = 'm'
	modeWhole  byte = 'w'
	modeState  byte = 's' // instances behave differently, members unchanged
)

var modeName = map[byte]string{modeMember: "member", modeWhole: "whole", modeState: "state"}

// Ctx: the receiver class known for `this` calls (-1: none), with the overridable methods the walk
// came up through (Runs, encoded by encodeRuns; "": none).
type Ctx struct {
	Recv int32
	Runs string
}

var noCtx = Ctx{-1, ""}

func (c Ctx) set() bool { return c.Recv >= 0 || c.Runs != "" }

// Key: a node of the walk with its mode and context.
type Key struct {
	N    int32
	Mode byte
	Ctx
}

func plainKey(n int32, mode byte) Key { return Key{n, mode, noCtx} }

func encodeRuns(ms []int32) string {
	slices.Sort(ms)
	ms = slices.Compact(ms)
	b := make([]byte, 0, 4*len(ms))
	for _, m := range ms {
		b = binary.LittleEndian.AppendUint32(b, uint32(m))
	}
	return string(b)
}

func decodeRuns(s string) []int32 {
	out := make([]int32, len(s)/4)
	for i := range out {
		out[i] = int32(binary.LittleEndian.Uint32([]byte(s[4*i:])))
	}
	return out
}

func addRun(runs string, m int32) string { return encodeRuns(append(decodeRuns(runs), m)) }

type Aff struct {
	Depth     int
	Parent    Key
	HasParent bool
	Why       string
}

type Parent struct {
	Key Key
	Why string
}

type Walk struct {
	Aff            map[Key]*Aff
	Keys           []Key // in the order the walk met them
	Screens        map[int32]Key
	ScreenOrder    []int32
	Components     map[int32]Key
	ComponentOrder []int32
}

func newWalk() *Walk {
	return &Walk{Aff: map[Key]*Aff{}, Screens: map[int32]Key{}, Components: map[int32]Key{}}
}

func (w *Walk) component(n int32, k Key) {
	if _, ok := w.Components[n]; !ok {
		w.Components[n] = k
		w.ComponentOrder = append(w.ComponentOrder, n)
	}
}

func (w *Walk) screen(n int32, k Key) {
	if _, ok := w.Screens[n]; !ok {
		w.Screens[n] = k
		w.ScreenOrder = append(w.ScreenOrder, n)
	}
}

func (g *Graph) isEnumEntry(n int32) bool {
	return g.kind(n) == "F" && g.flags(n)&snapshot.AccEnum != 0 && g.flags(g.owner(n))&snapshot.AccEnum != 0
}

// impact: the walk runs twice; the second skips the calls the first shows cannot reach the change
// (an enum entry switched on through a parameter, a default value of a passed argument).
func (g *Graph) impact(seeds []Seed, maxDepth int) *Walk {
	out := g.impactCut(seeds, maxDepth, nil)
	for _, k := range out.Keys {
		g.show(k.N)
	}
	return out
}

// impactCut: impact without the steps cut rejects (nil: none).
func (g *Graph) impactCut(seeds []Seed, maxDepth int, cut func(parent Key, child int32) bool) *Walk {
	var entries, rest []Seed
	for _, s := range seeds {
		if g.isEnumEntry(s.N) {
			entries = append(entries, s)
		}
	}
	for _, s := range seeds {
		if !slices.Contains(entries, s) {
			rest = append(rest, s)
		}
	}
	var groups [][]Seed
	for _, s := range entries {
		groups = append(groups, []Seed{s})
	}
	if len(rest) > 0 {
		groups = append(groups, rest)
	}
	out := newWalk()
	for _, group := range groups {
		plain := g.walk(group, maxDepth, nil, nil, nil, nil, cut)
		blocked := g.blockedDefaults(group, plain)
		if slices.Contains(entries, group[0]) {
			for k := range g.blockedCalls(group[0].N, plain) {
				blocked[k] = true
			}
		}
		res := plain
		if len(blocked) > 0 {
			res = g.walk(group, maxDepth, blocked, nil, nil, nil, cut)
		}
		for _, k := range res.Keys {
			if _, ok := out.Aff[k]; !ok {
				out.Aff[k] = res.Aff[k]
				out.Keys = append(out.Keys, k)
			}
		}
		for _, s := range res.ScreenOrder {
			out.screen(s, res.Screens[s])
		}
		for _, c := range res.ComponentOrder {
			out.component(c, res.Components[c])
		}
	}
	return out
}

// reachedImpls: a repo supertype method -> its implementations among the reached nodes.
func (g *Graph) reachedImpls(reached map[int32]bool) map[int32]map[int32]bool {
	impls := map[int32]map[int32]bool{}
	for n := range reached {
		if g.kind(n) == "M" {
			for _, sup := range g.overridden(n) {
				if impls[sup] == nil {
					impls[sup] = map[int32]bool{}
				}
				impls[sup][n] = true
			}
		}
	}
	return impls
}

type siteArg struct {
	Kind string
	Arg  int64
}

// callerSites: the call sites of one caller into a method: site -> argument position -> argument.
type callerSites struct {
	Order []int32
	Pos   map[int32]map[int64]siteArg
}

func (c *callerSites) get(site int32, pos int64) siteArg {
	if a, ok := c.Pos[site][pos]; ok {
		return a
	}
	return siteArg{"u", 0}
}

type sitesInto struct {
	Callers []int32
	By      map[int32]*callerSites
}

// sitesInto: the call sites into `target` by caller, in row order.
func (g *Graph) sitesInto(target int32) *sitesInto {
	if g.sites == nil {
		g.sites = map[int32][]int{}
		for r, d := range g.m.CallSite.Dst {
			n := g.m.Index(d)
			g.sites[n] = append(g.sites[n], r)
		}
	}
	out := &sitesInto{By: map[int32]*callerSites{}}
	c := &g.m.CallSite
	for _, r := range g.sites[target] {
		src := g.m.Index(c.Src[r])
		cs, ok := out.By[src]
		if !ok {
			cs = &callerSites{Pos: map[int32]map[int64]siteArg{}}
			out.By[src] = cs
			out.Callers = append(out.Callers, src)
		}
		if _, ok := cs.Pos[c.Site[r]]; !ok {
			cs.Pos[c.Site[r]] = map[int64]siteArg{}
			cs.Order = append(cs.Order, c.Site[r])
		}
		cs.Pos[c.Site[r]][int64(c.Pos[r])] = siteArg{c.Kind.At(r), c.Arg[r]}
	}
	return out
}

// conduits: the methods that depend on a changed enum entry only through a parameter.
type conduits struct {
	g        *Graph
	entry    int32
	entryID  int64
	switches map[int32]map[int64]bool // method -> the parameters it switches on over the entry
	swOrder  []int32
	direct   map[int32]bool // methods reading the entry itself
	reached  map[int32]bool
	impls    map[int32]map[int32]bool
	cache    map[int32]*sitesInto
	pos      map[int32]int64 // conduit -> the parameter the entry comes through
	order    []int32
}

// blockedCalls: calls that cannot reach the changed enum `entry`: {(caller, called, expanded method)}.
// A conduit depends on the entry only through a parameter; a call of it that passes another entry or
// null at every call site of that caller is blocked.
func (g *Graph) blockedCalls(entry int32, w *Walk) map[[3]int32]bool {
	c := &conduits{g: g, entry: entry, entryID: g.id(entry), switches: map[int32]map[int64]bool{},
		direct: map[int32]bool{}, reached: map[int32]bool{}, cache: map[int32]*sitesInto{}, pos: map[int32]int64{}}
	ps := &g.m.ParamSwitch
	for i := range ps.Method {
		if ps.Entry[i] == c.entryID {
			m := g.m.Index(ps.Method[i])
			if c.switches[m] == nil {
				c.switches[m] = map[int64]bool{}
				c.swOrder = append(c.swOrder, m)
			}
			c.switches[m][ps.Pos[i]] = true
		}
	}
	for i := range g.m.DirectRead.Method {
		if g.m.DirectRead.Entry[i] == c.entryID {
			c.direct[g.m.Index(g.m.DirectRead.Method[i])] = true
		}
	}
	if len(c.switches) == 0 {
		return nil
	}
	for k := range w.Aff {
		c.reached[k.N] = true
	}
	c.impls = g.reachedImpls(c.reached)
	c.find()
	return c.blocked()
}

func (c *conduits) into(t int32) *sitesInto {
	s, ok := c.cache[t]
	if !ok {
		s = c.g.sitesInto(t)
		c.cache[t] = s
	}
	return s
}

func (c *conduits) targetsOf(d int32) []int32 { return append([]int32{d}, c.g.overridden(d)...) }

// find: the conduits, from the switching methods out to their callers.
func (c *conduits) find() {
	var work []int32
	for _, m := range c.swOrder {
		if c.reached[m] {
			work = append(work, m)
		}
	}
	for len(work) > 0 {
		m := work[len(work)-1]
		work = work[:len(work)-1]
		if _, ok := c.pos[m]; ok {
			continue
		}
		pos, ok := c.evaluate(m)
		if !ok {
			continue
		}
		c.pos[m] = pos
		c.order = append(c.order, m)
		for _, t := range c.targetsOf(m) {
			for _, src := range c.into(t).Callers {
				if _, done := c.pos[src]; c.reached[src] && !done {
					work = append(work, src)
				}
			}
		}
	}
}

// evaluate: whether `m` is a conduit, and the one parameter the entry comes through.
func (c *conduits) evaluate(m int32) (int64, bool) {
	forwarded := map[int64]bool{}
	for p := range c.switches[m] {
		forwarded[p] = true
	}
	for _, u := range c.g.uses(m) {
		if u.N != m && !c.forwards(m, u, forwarded) {
			return 0, false
		}
	}
	if len(forwarded) == 1 {
		for p := range forwarded {
			return p, true
		}
	}
	return 0, false
}

// forwards: whether a use of `m` keeps it a conduit: it reads the entry where `m` switches on a
// parameter, reaches nothing changed, or calls conduits passing them parameters of `m` (added to
// `forwarded`).
func (c *conduits) forwards(m int32, u Use, forwarded map[int64]bool) bool {
	dst, kind := u.N, u.Kind
	if dst == c.entry && kind == snapshot.READ {
		_, sw := c.switches[m]
		return !c.direct[m] && sw
	}
	var hit []int32
	if c.reached[dst] {
		hit = append(hit, dst)
	}
	if kind == snapshot.CALL {
		for h := range c.impls[dst] {
			hit = append(hit, h)
		}
	}
	if len(hit) == 0 {
		return true
	}
	anyConduit := false
	for _, h := range hit {
		_, ok := c.pos[h]
		anyConduit = anyConduit || ok
	}
	if kind != snapshot.CALL || !anyConduit {
		return false
	}
	for _, h := range hit {
		pos, ok := c.pos[h]
		if !ok {
			return false
		}
		for _, t := range c.targetsOf(h) {
			cs, ok := c.into(t).By[m]
			if !ok {
				continue
			}
			for _, site := range cs.Order {
				a := cs.get(site, pos)
				if a.Kind == "p" {
					forwarded[a.Arg] = true
				} else if a.Kind == "u" || a.Kind == "c" && a.Arg == c.entryID {
					return false
				}
			}
		}
	}
	return true
}

// blocked: the calls of a conduit passing another entry or null at every site of the caller.
func (c *conduits) blocked() map[[3]int32]bool {
	blocked := map[[3]int32]bool{}
	for _, d := range c.order {
		pos := c.pos[d]
		for _, t := range c.targetsOf(d) {
			s := c.into(t)
			for _, src := range s.Callers {
				cs := s.By[src]
				all := len(cs.Order) > 0
				for _, site := range cs.Order {
					a := cs.get(site, pos)
					if !(a.Kind == "n" || a.Kind == "c" && a.Arg != c.entryID) {
						all = false
						break
					}
				}
				if all {
					blocked[[3]int32{src, t, d}] = true
				}
			}
		}
	}
	return blocked
}

// blockedDefaults: calls of a `$default` bridge that pass every argument whose default the change
// reaches: {(caller, bridge, bridge)}.
func (g *Graph) blockedDefaults(seeds []Seed, w *Walk) map[[3]int32]bool {
	if g.guards == nil {
		g.guards = map[int32]map[[2]int32]map[[2]int32]bool{}
		d := &g.m.DefaultGuard
		for i := range d.Method {
			m := g.m.Index(d.Method[i])
			if g.guards[m] == nil {
				g.guards[m] = map[[2]int32]map[[2]int32]bool{}
			}
			k := [2]int32{g.m.Index(d.Dst[i]), d.Kind[i]}
			if g.guards[m][k] == nil {
				g.guards[m][k] = map[[2]int32]bool{}
			}
			g.guards[m][k][[2]int32{d.Pos[i], d.Bit[i]}] = true
		}
	}
	blocked := map[[3]int32]bool{}
	reached := map[int32]bool{}
	for k := range w.Aff {
		reached[k.N] = true
	}
	seedIDs := map[int32]bool{}
	for _, s := range seeds {
		seedIDs[s.N] = true
	}
	var bridges []int32
	for n := range reached {
		if _, ok := g.guards[n]; ok && !seedIDs[n] {
			bridges = append(bridges, n)
		}
	}
	if len(bridges) == 0 {
		return blocked
	}
	impls := g.reachedImpls(reached)
	for _, d := range bridges {
		bits := map[[2]int32]bool{}
		for _, u := range g.uses(d) {
			_, impl := impls[u.N]
			impl = impl && u.Kind == snapshot.CALL
			if u.N == d || !(reached[u.N] || impl) {
				continue
			}
			gd := g.guards[d][[2]int32{u.N, u.Kind}]
			if len(gd) == 0 || impl {
				bits = nil
				break
			}
			for b := range gd {
				bits[b] = true
			}
		}
		if len(bits) == 0 {
			continue
		}
		s := g.sitesInto(d)
		for _, src := range s.Callers {
			cs := s.By[src]
			all := true
			for _, site := range cs.Order {
				for pb := range bits {
					a := cs.get(site, int64(pb[0]))
					if !(a.Kind == "i" && a.Arg&int64(pb[1]) == 0) {
						all = false
					}
				}
			}
			if all {
				blocked[[3]int32{src, d, d}] = true
			}
		}
	}
	return blocked
}

func (g *Graph) thisCall(caller, callee int32) bool {
	if nm := g.name(callee); nm == "<init>" || nm == "<clinit>" || g.flags(callee)&(accStatic|accPrivate) != 0 {
		return false
	}
	return caller >= 0 && (caller == g.owner(callee) || g.isSubtype(caller, g.owner(callee)))
}

// runsOverride: whether an instance of `cls` runs an override of one of `methods` instead.
func (g *Graph) runsOverride(cls int32, methods []int32) bool {
	if cls < 0 || len(methods) == 0 {
		return false
	}
	sups := g.supSet(cls)
	for _, m := range methods {
		for _, o := range g.overriders(m) {
			if o == cls || sups[o] {
				return true
			}
		}
	}
	return false
}

func (g *Graph) key(n int32, ctx Ctx) Key {
	switch g.kind(n) {
	case "C": // a class reached through a use of something else: its instances change
		if ctx.Runs != "" {
			return Key{n, modeState, ctx}
		}
		if _, _, ok := g.enclosing(g.name(n)); ctx.Recv >= 0 && ok {
			return Key{n, modeState, Ctx{ctx.Recv, ""}}
		}
		return plainKey(n, modeState)
	case "R", "ROUTE":
		return plainKey(n, modeWhole)
	}
	if ctx.set() {
		return Key{n, modeMember, ctx}
	}
	return plainKey(n, modeMember)
}

// walker is the state of one walk; a step reads one reached key.
type walker struct {
	g        *Graph
	w        *Walk
	queue    []Key
	maxDepth int
	blocked  map[[3]int32]bool
	skip     map[int32]bool
	bases    map[int32]bool
	alt      map[Key][]Parent
	cut      func(parent Key, child int32) bool
}

// step: a reached key, its decoded context and the depth of what it reaches.
type step struct {
	key   Key
	ctx   int32
	runs  []int32
	aff   *Aff
	depth int
	nd    int
}

// walk: maxDepth < 0: no limit; nil maps: none. alt: the other parents of
// a key at the depth of its first one; cut: the steps not taken.
func (g *Graph) walk(seeds []Seed, maxDepth int, blocked map[[3]int32]bool,
	skip map[int32]bool, bases map[int32]bool, alt map[Key][]Parent, cut func(Key, int32) bool) *Walk {
	wk := &walker{g: g, w: newWalk(), maxDepth: maxDepth, blocked: blocked, skip: skip, bases: bases, alt: alt, cut: cut}
	for _, s := range seeds {
		wk.push(plainKey(s.N, s.Mode), 0, nil, "changed")
	}
	for head := 0; head < len(wk.queue); head++ {
		wk.visit(wk.queue[head])
	}
	return wk.w
}

func (wk *walker) push(k Key, depth int, parent *Key, why string) {
	if wk.cut != nil && parent != nil && wk.cut(*parent, k.N) {
		return
	}
	if a, ok := wk.w.Aff[k]; !ok {
		v := &Aff{Depth: depth, Why: why}
		if parent != nil {
			v.Parent, v.HasParent = *parent, true
		}
		wk.w.Aff[k] = v
		wk.w.Keys = append(wk.w.Keys, k)
		wk.queue = append(wk.queue, k)
	} else if wk.alt != nil && parent != nil && a.Depth == depth && !(a.HasParent && a.Parent == *parent && a.Why == why) {
		p := Parent{*parent, why}
		if !slices.Contains(wk.alt[k], p) {
			wk.alt[k] = append(wk.alt[k], p)
		}
	}
}

func (wk *walker) isBlocked(src, called, expanded int32) bool {
	return wk.blocked[[3]int32{src, called, expanded}]
}

func (wk *walker) visit(key Key) {
	g, w, nid := wk.g, wk.w, key.N
	aff := w.Aff[key]
	s := &step{key: key, ctx: key.Recv, aff: aff, depth: aff.Depth, nd: aff.Depth + 1}
	if key.Runs != "" {
		s.runs = decodeRuns(key.Runs)
	}
	kind := g.kind(nid)
	top := g.topOf(nid)
	if wk.skip[nid] {
		return
	}
	// a component is reached, not a sink: what uses it (an Application's getters) is reached too
	if c := g.componentAt(nid); c >= 0 {
		w.component(c, key)
	}
	if kind == "ROUTE" {
		w.screen(nid, key)
		return
	}
	if rows, ok := g.screens[nid]; ok && rows[0].Kind == "preview" {
		w.screen(nid, key)
		return
	}
	if rows, ok := g.screens[nid]; ok && rows[0].Kind == "layout" {
		w.screen(nid, key)
	}
	if g.screenTops[top] {
		if route := g.routeContaining(nid); route >= 0 {
			wk.push(plainKey(route, modeWhole), s.depth, &key, "route content")
			return
		}
		w.screen(top, key)
		return
	}
	if g.diGlue[top] && s.depth > 0 {
		return
	}
	if wk.maxDepth >= 0 && s.depth >= wk.maxDepth {
		return
	}
	switch kind {
	case "M":
		wk.method(s)
	case "F":
		for _, u := range g.users(nid, snapshot.READ) {
			if !g.generatedRead(u.N, nid) {
				wk.push(g.key(u.N, Ctx{g.callerCtx(u.N, s.ctx), ""}), s.nd, &s.key, "reads")
			}
		}
		if g.flags(nid)&snapshot.ConstantFlag != 0 {
			for _, src := range g.constUsers(nid) {
				wk.push(plainKey(src, modeMember), s.nd, &s.key, "source")
			}
		}
	case "C":
		wk.class(s)
	case "R":
		for _, u := range g.users(nid, snapshot.RES, snapshot.XML_RES) {
			wk.push(g.key(u.N, noCtx), s.nd, &s.key, edgeKinds[u.Kind])
		}
	}
}

func (wk *walker) method(s *step) {
	g, nid, key := wk.g, s.key.N, &s.key
	owner := g.owner(nid)
	callers := g.users(nid, snapshot.CALL, snapshot.LAMBDA, snapshot.INLINE, snapshot.ROUTE_CONTENT)
	calledHere := wk.callers(s, owner, callers)
	sups := g.overridden(nid)
	wk.callsViaSuper(s, owner, sups)
	// a reached constructor: STATE below; a static initializer also changes the static fields it writes
	name := g.name(nid)
	if !(s.depth > 0 && name == "<init>") {
		for _, u := range g.uses(nid, snapshot.WRITE) {
			wk.push(g.key(u.N, Ctx{g.callerCtx(u.N, s.ctx), ""}), s.nd, key, "writes")
		}
	}
	// a method that only names a changed type runs as before: its class does not change state
	byType := s.aff.Why == "type"
	if byType && name == "<init>" && s.aff.HasParent {
		p := s.aff.Parent.N
		if g.kind(p) == "C" && strings.HasPrefix(g.name(p), g.name(owner)+"$") {
			byType = false // the constructor runs a lambda of its own class (init block)
		}
	}
	callback := func() bool {
		return len(sups) == 0 && g.flags(nid)&(accStatic|accPrivate) == 0 && g.hasExternalAncestor(owner)
	}
	switch {
	case byType:
	case name == "<init>" || name == "<clinit>":
		wk.push(g.key(owner, Ctx{s.ctx, ""}), s.nd, key, "initializer")
	case len(callers) == 0 && callback():
		wk.push(g.key(owner, Ctx{s.ctx, key.Runs}), s.nd, key, "framework callback")
	case s.ctx >= 0 && g.screenTops[s.ctx] && !calledHere && callback():
		// a base's `onCreate()` the screen does not override: the framework calls it on the screen
		wk.push(plainKey(s.ctx, modeState), s.nd, key, "framework callback")
	}
}

// callers: the callers of a reached method, reports whether one runs on the instance of the context.
func (wk *walker) callers(s *step, owner int32, callers []Use) bool {
	g, nid, key := wk.g, s.key.N, &s.key
	base := wk.bases != nil && owner >= 0 && g.subclassCount(owner) >= config.HubMin
	calledHere := false
	for _, u := range callers {
		src := u.N
		if wk.isBlocked(src, nid, nid) {
			continue
		}
		if base && g.isSubtype(g.callerClass(src), owner) {
			wk.bases[owner] = true // super.onCreate(), handleError() of a base of many classes
			continue
		}
		if !g.reaches(src, nid, s.ctx) {
			continue
		}
		calledHere = true
		if via, ok := g.callVia[nid][src]; ok && g.screenTops[via] && g.screens[via][0].Kind == "dialog" &&
			!g.kin(g.topOf(src), via) {
			// an inherited method called on a dialog (`show()`) runs in that dialog
			wk.push(g.key(via, noCtx), s.nd, key, "call on screen")
			continue
		}
		caller := g.callerClass(src)
		if g.runsOverride(caller, s.runs) {
			continue
		}
		sub := g.callerCtx(src, s.ctx)
		if g.runsOverride(sub, s.runs) {
			continue
		}
		through := ""
		if sub >= 0 || g.thisCall(caller, nid) { // the same instance
			through = key.Runs
			if u.Kind == snapshot.CALL && len(g.overriders(nid)) > 0 && !g.runsOverride(caller, []int32{nid}) {
				through = addRun(key.Runs, nid)
			}
		}
		wk.push(g.key(src, Ctx{sub, through}), s.nd, key, edgeKinds[u.Kind])
	}
	return calledHere
}

// callsViaSuper: calls through a supertype reach this implementation when the instance comes from
// DI (or from nowhere visible); an instance the code creates changes the behaviour where created.
func (wk *walker) callsViaSuper(s *step, owner int32, sups []int32) {
	g, nid, key := wk.g, s.key.N, &s.key
	if len(sups) == 0 {
		return
	}
	receiver := s.ctx
	if receiver < 0 {
		receiver = owner
	}
	var madeBy []int32
	di := false
	if g.flags(owner)&snapshot.AccAbstract == 0 {
		madeBy, di = g.creators(owner)
	}
	for _, src := range madeBy {
		wk.push(g.key(src, noCtx), s.nd, key, "creates "+g.label(owner))
	}
	if !di && len(madeBy) > 0 {
		return
	}
	type superCall struct {
		u       Use
		holders []int32
	}
	var viaSuper []superCall
	for _, sup := range sups {
		holders := g.holders(owner, g.owner(sup))
		for _, u := range g.users(sup, snapshot.CALL, snapshot.LAMBDA, snapshot.INLINE) {
			if g.reaches(u.N, sup, receiver) && !wk.isBlocked(u.N, sup, nid) &&
				!g.runsOverride(g.callerClass(u.N), s.runs) {
				viaSuper = append(viaSuper, superCall{u, holders})
			}
		}
	}
	for _, sc := range viaSuper {
		u := sc.u
		for _, rcv := range g.holdersIn(g.callerClass(u.N), receiver, sc.holders) {
			sub := g.callerCtx(u.N, rcv)
			if g.runsOverride(sub, s.runs) {
				continue
			}
			through := ""
			if sub >= 0 || g.thisCall(g.callerClass(u.N), nid) {
				through = key.Runs
			}
			wk.push(g.key(u.N, Ctx{sub, through}), s.nd, key, "call via supertype")
		}
	}
}

func (wk *walker) class(s *step) {
	g, nid, key := wk.g, s.key.N, &s.key
	if s.key.Mode == modeWhole {
		for _, m := range g.members(nid) {
			wk.push(plainKey(m, modeMember), s.depth, key, "member")
		}
	}
	_, _, lambda := g.enclosing(g.name(nid))
	lambdaCtx := int32(-1)
	if lambda { // a lambda of a `this` call
		lambdaCtx = s.ctx
	}
	if wk.bases != nil && !lambda && s.aff.Why == "framework callback" && !g.screenTops[nid] {
		wk.creatorsOfCallback(s)
	}
	base := wk.bases != nil && g.subclassCount(nid) >= config.HubMin
	if base { // a base of many classes: listed, its subclasses not walked
		wk.bases[nid] = true
	}
	for _, u := range g.users(nid, snapshot.TYPE, snapshot.EXTENDS, snapshot.IMPLEMENTS, snapshot.XML_CLASS, snapshot.ROUTE_CONTENT) {
		if base && (u.Kind == snapshot.EXTENDS || u.Kind == snapshot.IMPLEMENTS) {
			continue
		}
		if wk.bases != nil && u.Kind == snapshot.TYPE && !lambda && !(g.screenTops[g.topOf(u.N)] && g.isViewModel(nid)) {
			continue // who opens: code that runs it, not code naming its type
		}
		if len(s.runs) > 0 && u.Kind == snapshot.EXTENDS { // a subclass inherits the callback: unless it overrides
			if !g.runsOverride(u.N, s.runs) {
				wk.push(Key{u.N, modeState, Ctx{-1, key.Runs}}, s.nd, key, edgeKinds[u.Kind])
			}
			continue
		}
		wk.push(g.key(u.N, Ctx{g.callerCtx(u.N, lambdaCtx), ""}), s.nd, key, edgeKinds[u.Kind])
	}
}

// creatorsOfCallback: the framework calls back an object the code makes (an ActivityResultContract's
// createIntent): who makes it runs the callback; a subclass's super() makes no object.
func (wk *walker) creatorsOfCallback(s *step) {
	g, nid := wk.g, s.key.N
	for _, m := range g.members(nid) {
		if g.name(m) != "<init>" {
			continue
		}
		for _, u := range g.users(m, snapshot.CALL) {
			if g.topOf(u.N) != g.topOf(nid) && !g.diGlue[g.topOf(u.N)] && !g.isSubtype(g.callerClass(u.N), nid) {
				wk.push(g.key(u.N, noCtx), s.nd, &s.key, "creates "+g.label(nid))
			}
		}
	}
}

type Step struct {
	N   int32
	Why string
}

// path: (node, edge label) from the changed node to `key`, merged by label.
func (g *Graph) path(w *Walk, k Key) []Step {
	var out []Step
	for {
		a := w.Aff[k]
		out = append(out, Step{k.N, a.Why})
		if !a.HasParent {
			break
		}
		k = a.Parent
	}
	slices.Reverse(out)
	return g.merged(out)
}

func (g *Graph) merged(steps []Step) []Step {
	var out []Step
	for _, s := range steps {
		if len(out) > 0 && g.label(out[len(out)-1].N) == g.label(s.N) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// before: the order of keys met by a walk: by depth, then node id, mode, receiver id and the ids of
// the overridable methods the walk came up through.
func (g *Graph) before(w *Walk, a, b Key) bool {
	if da, db := w.Aff[a].Depth, w.Aff[b].Depth; da != db {
		return da < db
	}
	if a.N != b.N {
		return g.id(a.N) < g.id(b.N)
	}
	if a.Mode != b.Mode {
		return modeName[a.Mode] < modeName[b.Mode]
	}
	if a.Recv != b.Recv {
		return g.idOrNone(a.Recv) < g.idOrNone(b.Recv)
	}
	return slices.Compare(g.runIDs(a.Runs), g.runIDs(b.Runs)) < 0
}

func (g *Graph) idOrNone(n int32) int64 {
	if n < 0 {
		return -1
	}
	return g.id(n)
}

func (g *Graph) runIDs(runs string) []int64 {
	var ids []int64
	for _, m := range decodeRuns(runs) {
		ids = append(ids, g.id(m))
	}
	slices.Sort(ids)
	return ids
}

// holdersIn: the receivers a call through a supertype in class `caller` runs on, when the instance
// comes from DI to `holders`: in its own code and the holders'; in a base of holders, each holder
// below it (the base gets the instance from the holder: `provideProvider()`); elsewhere, none.
func (g *Graph) holdersIn(caller, receiver int32, holders []int32) []int32 {
	if holders == nil || g.kin(caller, receiver) { // or a `this` call
		return []int32{receiver}
	}
	var below []int32
	for _, h := range holders {
		if h == caller || g.isSubtype(caller, h) {
			return []int32{receiver}
		}
		if g.isSubtype(h, caller) {
			below = append(below, h)
		}
	}
	return below
}

// componentAt: the component a node is in: its class's, else its top-level class's; -1: none.
func (g *Graph) componentAt(n int32) int32 {
	c := n
	if k := g.kind(n); k == "M" || k == "F" {
		c = g.owner(n)
	}
	if _, ok := g.components[c]; ok {
		return c
	}
	if t := g.topOf(n); t != c {
		if _, ok := g.components[t]; ok {
			return t
		}
	}
	return -1
}
