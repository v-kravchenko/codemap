package main

// impact: the walk from changed nodes to everything that may change (port of graph.py impact, _impact).

import (
	"encoding/binary"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	modeMember byte = 'm'
	modeWhole  byte = 'w'
	modeState  byte = 's' // instances behave differently, members unchanged
)

var modeName = map[byte]string{modeMember: "member", modeWhole: "whole", modeState: "state"}

// Ctx: the receiver class known for `this` calls (-1: none), with the overridable methods the walk
// came up through (Runs, encoded by encodeRuns; "": none): graph.py's ctx, ("runs", runs, receiver).
type Ctx struct {
	Recv int32
	Runs string
}

var noCtx = Ctx{-1, ""}

func (c Ctx) set() bool { return c.Recv >= 0 || c.Runs != "" }

// Key: a node of the walk with its mode; a context makes graph.py's (node, (mode, ctx)).
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
	Aff         map[Key]*Aff
	Keys        []Key // in the order the walk met them
	Screens     map[int32]Key
	ScreenOrder []int32
}

func newWalk() *Walk { return &Walk{Aff: map[Key]*Aff{}, Screens: map[int32]Key{}} }

func (w *Walk) screen(n int32, k Key) {
	if _, ok := w.Screens[n]; !ok {
		w.Screens[n] = k
		w.ScreenOrder = append(w.ScreenOrder, n)
	}
}

// repr: the Python repr of the key, the tie-break between keys of one node and depth.
func (g *Graph) repr(k Key) string {
	mode := "'" + modeName[k.Mode] + "'"
	if !k.Ctx.set() {
		return fmt.Sprintf("(%d, %s)", g.id(k.N), mode)
	}
	recv := "None"
	if k.Recv >= 0 {
		recv = strconv.FormatInt(g.id(k.Recv), 10)
	}
	if k.Runs == "" {
		return fmt.Sprintf("(%d, (%s, %s))", g.id(k.N), mode, recv)
	}
	return fmt.Sprintf("(%d, (%s, ('runs', frozenset({%s}), %s)))", g.id(k.N), mode, g.frozenset(k.Runs), recv)
}

// ponytail: CPython's order of a small int frozenset (slot hash & 7, no collisions), exact for up to
// 4 ids without slot collisions; emulate the probing if ties between such keys ever differ.
func (g *Graph) frozenset(runs string) string {
	ids := []int64{}
	for _, m := range decodeRuns(runs) {
		ids = append(ids, g.id(m))
	}
	mask := int64(7)
	for n := int64(len(ids)); n*5 >= mask*3; {
		mask = mask*2 + 1
	}
	hash := func(v int64) int64 { return v % (1<<61 - 1) }
	sort.Slice(ids, func(i, j int) bool {
		a, b := hash(ids[i])&mask, hash(ids[j])&mask
		if a != b {
			return a < b
		}
		return ids[i] < ids[j]
	})
	parts := make([]string, len(ids))
	for i, v := range ids {
		parts[i] = strconv.FormatInt(v, 10)
	}
	return strings.Join(parts, ", ")
}

func (g *Graph) isEnumEntry(n int32) bool {
	return g.kind(n) == "F" && g.flags(n)&accEnum != 0 && g.flags(g.owner(n))&accEnum != 0
}

// impact: the walk runs twice; the second skips the calls the first shows cannot reach the change
// (an enum entry switched on through a parameter, a default value of a passed argument).
func (g *Graph) impact(seeds []Seed, maxDepth int) *Walk {
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
		plain := g.walk(group, maxDepth, nil, nil, nil, nil, nil)
		blocked := g.blockedDefaults(group, plain)
		if slices.Contains(entries, group[0]) {
			for k := range g.blockedCalls(group[0].N, plain) {
				blocked[k] = true
			}
		}
		res := plain
		if len(blocked) > 0 {
			res = g.walk(group, maxDepth, nil, blocked, nil, nil, nil)
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
	}
	for _, k := range out.Keys {
		g.show(k.N)
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

// blockedCalls: calls that cannot reach the changed enum `entry`: {(caller, called, expanded method)}.
// A conduit depends on the entry only through a parameter; a call of it that passes another entry or
// null at every call site of that caller is blocked.
func (g *Graph) blockedCalls(entry int32, w *Walk) map[[3]int32]bool {
	entryID := g.id(entry)
	switches := map[int32]map[int64]bool{}
	var swOrder []int32
	ps := &g.m.ParamSwitch
	for i := range ps.Method {
		if ps.Entry[i] == entryID {
			m := g.m.Index(ps.Method[i])
			if switches[m] == nil {
				switches[m] = map[int64]bool{}
				swOrder = append(swOrder, m)
			}
			switches[m][ps.Pos[i]] = true
		}
	}
	direct := map[int32]bool{}
	for i := range g.m.DirectRead.Method {
		if g.m.DirectRead.Entry[i] == entryID {
			direct[g.m.Index(g.m.DirectRead.Method[i])] = true
		}
	}
	if len(switches) == 0 {
		return nil
	}
	reached := map[int32]bool{}
	for k := range w.Aff {
		reached[k.N] = true
	}
	impls := g.reachedImpls(reached)
	cache := map[int32]*sitesInto{}
	into := func(t int32) *sitesInto {
		s, ok := cache[t]
		if !ok {
			s = g.sitesInto(t)
			cache[t] = s
		}
		return s
	}
	targetsOf := func(d int32) []int32 { return append([]int32{d}, g.overridden(d)...) }
	conduit := map[int32]int64{}
	evaluate := func(m int32) (int64, bool) {
		forwarded := map[int64]bool{}
		for p := range switches[m] {
			forwarded[p] = true
		}
		for _, u := range g.uses(m) {
			dst, kind := u.N, u.Kind
			if dst == m {
				continue
			}
			if dst == entry && kind == READ {
				if _, sw := switches[m]; direct[m] || !sw {
					return 0, false
				}
				continue
			}
			var hit []int32
			if reached[dst] {
				hit = append(hit, dst)
			}
			if kind == CALL {
				for h := range impls[dst] {
					hit = append(hit, h)
				}
			}
			if len(hit) == 0 {
				continue
			}
			anyConduit := false
			for _, h := range hit {
				_, ok := conduit[h]
				anyConduit = anyConduit || ok
			}
			if kind != CALL || !anyConduit {
				return 0, false
			}
			for _, h := range hit {
				pos, ok := conduit[h]
				if !ok {
					return 0, false
				}
				for _, t := range targetsOf(h) {
					cs, ok := into(t).By[m]
					if !ok {
						continue
					}
					for _, site := range cs.Order {
						a := cs.get(site, pos)
						if a.Kind == "p" {
							forwarded[a.Arg] = true
						} else if a.Kind == "u" || a.Kind == "c" && a.Arg == entryID {
							return 0, false
						}
					}
				}
			}
		}
		if len(forwarded) == 1 {
			for p := range forwarded {
				return p, true
			}
		}
		return 0, false
	}
	var work []int32
	for _, m := range swOrder {
		if reached[m] {
			work = append(work, m)
		}
	}
	var conduitOrder []int32
	for len(work) > 0 {
		m := work[len(work)-1]
		work = work[:len(work)-1]
		if _, ok := conduit[m]; ok {
			continue
		}
		pos, ok := evaluate(m)
		if !ok {
			continue
		}
		conduit[m] = pos
		conduitOrder = append(conduitOrder, m)
		for _, t := range targetsOf(m) {
			for _, src := range into(t).Callers {
				if _, done := conduit[src]; reached[src] && !done {
					work = append(work, src)
				}
			}
		}
	}
	blocked := map[[3]int32]bool{}
	for _, d := range conduitOrder {
		pos := conduit[d]
		for _, t := range targetsOf(d) {
			s := into(t)
			for _, src := range s.Callers {
				cs := s.By[src]
				all := len(cs.Order) > 0
				for _, site := range cs.Order {
					a := cs.get(site, pos)
					if !(a.Kind == "n" || a.Kind == "c" && a.Arg != entryID) {
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
			impl = impl && u.Kind == CALL
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

// walk: graph.py _impact. maxDepth < 0: no limit; nil maps: graph.py None. alt: the other parents of
// a key at the depth of its first one.
func (g *Graph) walk(seeds []Seed, maxDepth int, stopAt map[int32]bool, blocked map[[3]int32]bool,
	skip map[int32]bool, bases map[int32]bool, alt map[Key][]Parent) *Walk {
	w := newWalk()
	queue := []Key{}
	push := func(k Key, depth int, parent *Key, why string) {
		if a, ok := w.Aff[k]; !ok {
			v := &Aff{Depth: depth, Why: why}
			if parent != nil {
				v.Parent, v.HasParent = *parent, true
			}
			w.Aff[k] = v
			w.Keys = append(w.Keys, k)
			queue = append(queue, k)
		} else if alt != nil && parent != nil && a.Depth == depth && !(a.HasParent && a.Parent == *parent && a.Why == why) {
			p := Parent{*parent, why}
			if !slices.Contains(alt[k], p) {
				alt[k] = append(alt[k], p)
			}
		}
	}
	isBlocked := func(src, called, expanded int32) bool { return blocked[[3]int32{src, called, expanded}] }
	for _, s := range seeds {
		push(plainKey(s.N, s.Mode), 0, nil, "changed")
	}
	for head := 0; head < len(queue); head++ {
		key := queue[head]
		nid, mode := key.N, key.Mode
		ctx := key.Recv
		var runs []int32
		if key.Runs != "" {
			runs = decodeRuns(key.Runs)
		}
		aff := w.Aff[key]
		depth := aff.Depth
		kind := g.kind(nid)
		top := g.topOf(nid)
		if stopAt != nil && stopAt[nid] {
			break
		}
		if skip[nid] {
			continue
		}
		if kind == "ROUTE" {
			w.screen(nid, key)
			continue
		}
		if rows, ok := g.screens[nid]; ok && rows[0].Kind == "preview" {
			w.screen(nid, key)
			continue
		}
		if rows, ok := g.screens[nid]; ok && rows[0].Kind == "layout" {
			w.screen(nid, key)
		}
		if g.screenTops[top] {
			if route := g.routeContaining(nid); route >= 0 {
				push(plainKey(route, modeWhole), depth, &key, "route content")
				continue
			}
			w.screen(top, key)
			continue
		}
		if g.diGlue[top] && depth > 0 {
			continue
		}
		if maxDepth >= 0 && depth >= maxDepth {
			continue
		}
		nd := depth + 1
		switch kind {
		case "M":
			owner := g.owner(nid)
			callers := g.users(nid, CALL, LAMBDA, INLINE, ROUTE_CONTENT)
			base := bases != nil && owner >= 0 && g.subclassCount(owner) >= hubMin
			calledHere := false // a caller runs on the instance of `ctx`
			for _, u := range callers {
				src := u.N
				if isBlocked(src, nid, nid) {
					continue
				}
				if base && g.isSubtype(g.callerClass(src), owner) {
					bases[owner] = true // super.onCreate(), handleError() of a base of many classes
					continue
				}
				if !g.reaches(src, nid, ctx) {
					continue
				}
				calledHere = true
				if via, ok := g.callVia[nid][src]; ok && g.screenTops[via] && g.screens[via][0].Kind == "dialog" &&
					!g.kin(g.topOf(src), via) {
					// an inherited method called on a dialog (`show()`) runs in that dialog
					push(g.key(via, noCtx), nd, &key, "call on screen")
					continue
				}
				caller := g.callerClass(src)
				if g.runsOverride(caller, runs) {
					continue
				}
				sub := g.callerCtx(src, ctx)
				if g.runsOverride(sub, runs) {
					continue
				}
				through := ""
				if sub >= 0 || g.thisCall(caller, nid) { // the same instance
					through = key.Runs
					if u.Kind == CALL && len(g.overriders(nid)) > 0 && !g.runsOverride(caller, []int32{nid}) {
						through = addRun(key.Runs, nid)
					}
				}
				push(g.key(src, Ctx{sub, through}), nd, &key, edgeKinds[u.Kind])
			}
			// calls through a supertype reach this implementation when the instance comes from DI (or
			// from nowhere visible); an instance the code creates changes the behaviour where created
			type superCall struct {
				u       Use
				holders []int32
			}
			var viaSuper []superCall
			receiver := ctx
			if receiver < 0 {
				receiver = owner
			}
			sups := g.overridden(nid)
			if len(sups) > 0 {
				var madeBy []int32
				di := false
				if g.flags(owner)&accAbstract == 0 {
					madeBy, di = g.creators(owner)
				}
				for _, src := range madeBy {
					push(g.key(src, noCtx), nd, &key, "creates "+g.label(owner))
				}
				if di || len(madeBy) == 0 {
					for _, sup := range sups {
						holders := g.holders(owner, g.owner(sup))
						for _, u := range g.users(sup, CALL, LAMBDA, INLINE) {
							if g.reaches(u.N, sup, receiver) && !isBlocked(u.N, sup, nid) &&
								!g.runsOverride(g.callerClass(u.N), runs) {
								viaSuper = append(viaSuper, superCall{u, holders})
							}
						}
					}
				}
			}
			for _, sc := range viaSuper {
				u := sc.u
				for _, rcv := range g.holdersIn(g.callerClass(u.N), receiver, sc.holders) {
					sub := g.callerCtx(u.N, rcv)
					if g.runsOverride(sub, runs) {
						continue
					}
					through := ""
					if sub >= 0 || g.thisCall(g.callerClass(u.N), nid) {
						through = key.Runs
					}
					push(g.key(u.N, Ctx{sub, through}), nd, &key, "call via supertype")
				}
			}
			// a reached constructor: STATE below; a static initializer also changes the static fields it writes
			name := g.name(nid)
			if !(depth > 0 && name == "<init>") {
				for _, u := range g.uses(nid, WRITE) {
					push(g.key(u.N, Ctx{g.callerCtx(u.N, ctx), ""}), nd, &key, "writes")
				}
			}
			// a method that only names a changed type runs as before: its class does not change state
			byType := aff.Why == "type"
			if byType && name == "<init>" && aff.HasParent {
				p := aff.Parent.N
				if g.kind(p) == "C" && strings.HasPrefix(g.name(p), g.name(owner)+"$") {
					byType = false // the constructor runs a lambda of its own class (init block)
				}
			}
			if byType {
			} else if name == "<init>" || name == "<clinit>" {
				push(g.key(owner, Ctx{ctx, ""}), nd, &key, "initializer")
			} else if len(callers) == 0 && len(sups) == 0 && g.flags(nid)&(accStatic|accPrivate) == 0 &&
				g.hasExternalAncestor(owner) {
				push(g.key(owner, Ctx{ctx, key.Runs}), nd, &key, "framework callback")
			} else if ctx >= 0 && g.screenTops[ctx] && !calledHere && len(sups) == 0 &&
				g.flags(nid)&(accStatic|accPrivate) == 0 && g.hasExternalAncestor(owner) {
				// a base's `onCreate()` the screen does not override: the framework calls it on the screen
				push(plainKey(ctx, modeState), nd, &key, "framework callback")
			}
		case "F":
			for _, u := range g.users(nid, READ) {
				if !g.generatedRead(u.N, nid) {
					push(g.key(u.N, Ctx{g.callerCtx(u.N, ctx), ""}), nd, &key, "reads")
				}
			}
			if g.flags(nid)&constantFlag != 0 {
				for _, src := range g.constUsers(nid) {
					push(plainKey(src, modeMember), nd, &key, "source")
				}
			}
		case "C":
			if mode == modeWhole {
				for _, m := range g.members(nid) {
					push(plainKey(m, modeMember), depth, &key, "member")
				}
			}
			_, _, lambda := g.enclosing(g.name(nid))
			lambdaCtx := int32(-1)
			if lambda { // a lambda of a `this` call
				lambdaCtx = ctx
			}
			base := bases != nil && g.subclassCount(nid) >= hubMin
			if base { // a base of many classes: listed, its subclasses not walked
				bases[nid] = true
			}
			for _, u := range g.users(nid, TYPE, EXTENDS, IMPLEMENTS, XML_CLASS, ROUTE_CONTENT) {
				if base && (u.Kind == EXTENDS || u.Kind == IMPLEMENTS) {
					continue
				}
				if bases != nil && u.Kind == TYPE && !lambda && !(g.screenTops[g.topOf(u.N)] && g.isViewModel(nid)) {
					continue // who opens: code that runs it, not code naming its type
				}
				if len(runs) > 0 && u.Kind == EXTENDS { // a subclass inherits the callback: unless it overrides
					if !g.runsOverride(u.N, runs) {
						push(Key{u.N, modeState, Ctx{-1, key.Runs}}, nd, &key, edgeKinds[u.Kind])
					}
					continue
				}
				push(g.key(u.N, Ctx{g.callerCtx(u.N, lambdaCtx), ""}), nd, &key, edgeKinds[u.Kind])
			}
		case "R":
			for _, u := range g.users(nid, RES, XML_RES) {
				push(g.key(u.N, noCtx), nd, &key, edgeKinds[u.Kind])
			}
		}
	}
	return w
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

// before: graph.py's (depth, repr(key)) order.
func (g *Graph) before(w *Walk, a, b Key) bool {
	da, db := w.Aff[a].Depth, w.Aff[b].Depth
	if da != db {
		return da < db
	}
	return g.repr(a) < g.repr(b)
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
