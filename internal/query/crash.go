package query

// crash: where a crash's stack trace ran in the code map, and the screens and components it ran
// for (docs/decisions/0029). Each own frame is walked as impact walks a symbol; the frame reaching
// the fewest screens and components names them, since every frame of the trace ran for the same one.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/v-kravchenko/codemap/internal/stack"
)

// frame statuses: in the map; not in it, though its package is (removed, or made by R8); generated
// DI code the map leaves out; a library's
const (
	frameResolved  = "resolved"
	frameNotFound  = "not_found"
	frameGenerated = "generated"
	frameLibrary   = "library"
)

type crashFrame struct {
	stack.Frame
	exception int // in the chain, 0 the outermost
	status    string
	seed      Seed
	walk      *Walk
	reach     int // screens and components the walk reaches
}

type crashAnswer struct {
	trace                      stack.Trace
	frames                     []*crashFrame
	crashedAt, entry, anchor   int // frame indexes, -1: none
	impact                     *impactAnswer
	warnings                   []string
	mapping, obfuscated, named bool // --mapping given; frames left in R8 names; frames a mapping renamed
}

func (g *Graph) cmdCrash(o opts) int {
	input := "-"
	if len(o.pos) > 0 {
		input = o.pos[0]
	}
	tr, ok := stack.Parse(readInput(input))
	if !ok {
		if input == "-" {
			input = "stdin"
		}
		panic(queryErr("input", "No stack trace in "+input+": no line `at Class.method(File:N)` under an exception"))
	}
	c := &crashAnswer{trace: tr, crashedAt: -1, entry: -1, anchor: -1, mapping: o.mapping != ""}
	var m *stack.Mapping
	if c.mapping {
		var err error
		if m, err = stack.ReadMapping(strings.NewReader(readInput(o.mapping))); err != nil {
			panic(queryErr("input", "cannot read "+o.mapping+": "+err.Error()))
		}
	}
	walks := map[Seed]*Walk{}
	for i, e := range tr.Exceptions {
		for _, f := range e.Frames {
			retraced := []stack.Frame{f}
			if m != nil {
				retraced = m.Retrace(f)
			}
			for _, r := range retraced {
				cf := &crashFrame{Frame: r, exception: i, status: frameLibrary}
				c.named = c.named || r.Obfuscated != nil
				if s, ok := g.frameSeed(r); ok {
					cf.status, cf.seed = frameResolved, s
					if walks[s] == nil {
						walks[s] = g.impact([]Seed{s}, o.depth)
					}
					cf.walk = walks[s]
					cf.reach = len(cf.walk.Screens) + len(cf.walk.Components)
				} else if isDIGlue(strings.ReplaceAll(r.Class, ".", "/")) {
					cf.status = frameGenerated
				} else if g.packageInMap(r.Class) {
					cf.status = frameNotFound
				}
				c.obfuscated = c.obfuscated || r.File == "SourceFile" && cf.status != frameResolved
				c.frames = append(c.frames, cf)
			}
		}
	}
	c.pick()
	c.warn()
	a := &impactAnswer{w: newWalk(), mf: g.moduleFilter(o.modules), navs: map[int32]*navOf{}}
	if c.anchor >= 0 {
		a.seeds, a.w = []Seed{c.frames[c.anchor].seed}, c.frames[c.anchor].walk
	}
	a.via = g.screenVia(a.seeds, o.depth, a.w)
	g.impactScreens(a, o)
	g.impactComponents(a)
	if o.nav {
		for _, items := range a.byKind {
			for _, it := range items {
				if screenKinds[g.screens[it.n][0].Kind] {
					a.navs[it.n] = g.navOfScreen(it.n)
				}
			}
		}
	}
	c.impact = a
	if o.json {
		g.crashJSON(c)
	} else {
		g.crashText(c, o)
	}
	return exitOK
}

// pick: where it crashed, the deepest own frame of the innermost cause that has one; the entry,
// the outermost own frame of the outermost exception that has one; the anchor, the own frame
// reaching the fewest screens and components, nearest the crash on a tie.
func (c *crashAnswer) pick() {
	for i, f := range c.frames {
		if f.status != frameResolved {
			continue
		}
		if c.crashedAt < 0 || f.exception > c.frames[c.crashedAt].exception {
			c.crashedAt = i
		}
		if c.entry < 0 || f.exception == c.frames[c.entry].exception {
			c.entry = i
		}
		if f.reach > 0 && (c.anchor < 0 || f.reach < c.frames[c.anchor].reach) {
			c.anchor = i
		}
	}
}

func (c *crashAnswer) warn() {
	switch {
	case c.crashedAt < 0 && c.obfuscated && !c.mapping:
		c.warnings = append(c.warnings, "the trace is obfuscated (SourceFile): give the build's mapping.txt with --mapping")
	case c.crashedAt < 0:
		c.warnings = append(c.warnings, "no frame of the trace is in the code map: the crash is in a library or the platform, or the map is of another app")
	case c.anchor < 0:
		c.warnings = append(c.warnings, "no own frame reaches a screen or component")
	}
	if c.mapping && !c.named {
		c.warnings = append(c.warnings, "the mapping renames no frame of the trace: it may be another build's")
	}
}

// frameSeed: the method a frame ran in: a member of its class, or of the class enclosing it
// (`$lambda`, R8's `$$ExternalSyntheticLambda0`), whose lines hold the frame's line, else the one
// of its name; the class when neither tells.
func (g *Graph) frameSeed(f stack.Frame) (Seed, bool) {
	name := strings.ReplaceAll(f.Class, ".", "/")
	c, ok := g.classByName[name]
	for !ok {
		i := strings.LastIndexByte(name, '$')
		if i < 0 {
			return Seed{}, false
		}
		name = strings.TrimRight(name[:i], "$")
		c, ok = g.classByName[name]
	}
	nd := &g.m.Node
	ln, best := int32(f.Line), int32(-1)
	if ln > 0 {
		for _, n := range g.members(c) {
			if g.kind(n) == "M" && nd.LMin[n] != 0 && nd.LMin[n] <= ln && ln <= nd.LMax[n] &&
				(best < 0 || nd.LMax[n]-nd.LMin[n] < nd.LMax[best]-nd.LMin[best] ||
					nd.LMax[n]-nd.LMin[n] == nd.LMax[best]-nd.LMin[best] && g.sname(n) == f.Method) {
				best = n
			}
		}
	}
	if best < 0 {
		method, _, _ := strings.Cut(f.Method, "-") // Compose and value classes mangle: `doCompose-aFTiNEg`
		for _, n := range g.members(c) {
			if g.kind(n) == "M" && (g.sname(n) == f.Method || g.sname(n) == method) {
				best = n
				break
			}
		}
	}
	if best < 0 {
		return Seed{c, modeWhole}, true
	}
	return Seed{best, modeMember}, true
}

// packageInMap: whether a class of the frame's package is in the map, so the frame is the
// project's though its class is not.
func (g *Graph) packageInMap(class string) bool {
	i := strings.LastIndexByte(class, '.')
	if i < 0 {
		return false
	}
	pkg := strings.ReplaceAll(class[:i+1], ".", "/")
	for name := range g.classByName {
		if strings.HasPrefix(name, pkg) && !strings.Contains(name[len(pkg):], "/") {
			return true
		}
	}
	return false
}

// frameLocation: the source file of the frame's method with the trace's line; the method's own
// location when the line is not in it (inlined code reports the inline function's file).
func (g *Graph) frameLocation(f *crashFrame) string {
	n, nd := f.seed.N, &g.m.Node
	if path := g.filePath[g.fileOf(n)]; path != "" && f.Line > 0 && nd.LMin[n] != 0 &&
		nd.LMin[n] <= int32(f.Line) && int32(f.Line) <= nd.LMax[n] {
		g.show(n)
		return path + ":" + strconv.Itoa(f.Line)
	}
	if loc := g.location(n); loc != "" {
		return loc
	}
	return traceLine(f.Frame)[len(f.Class)+len(f.Method)+2:] // no source in the project: the trace's (File:N)
}

// frameIn: the component or screen class the frame's code is in, as (kind, name); "" for none.
func (g *Graph) frameIn(f *crashFrame) (string, string) {
	if c := g.componentAt(f.seed.N); c >= 0 {
		return g.components[c], g.label(c)
	}
	if t := g.topOf(f.seed.N); g.screenTops[t] {
		return g.screens[t][0].Kind, g.screenName(t)
	}
	return "", ""
}

func traceLine(f stack.Frame) string {
	loc := f.File
	if f.Line > 0 {
		loc += ":" + strconv.Itoa(f.Line)
	}
	return fmt.Sprintf("%s.%s(%s)", f.Class, f.Method, loc)
}

func (g *Graph) crashText(c *crashAnswer, o opts) {
	e := c.trace.Exceptions
	fmt.Println("Crash: " + strings.TrimSuffix(e[0].Class+": "+e[0].Message, ": "))
	for _, x := range e[1:] {
		fmt.Println("  caused by " + strings.TrimSuffix(x.Class+": "+x.Message, ": "))
	}
	at := func(title string, i int) {
		if i < 0 {
			fmt.Printf("%-12s not in the code map\n", title)
			return
		}
		f := c.frames[i]
		in := ""
		if kind, name := g.frameIn(f); kind != "" {
			in = "  (" + kind + " " + name + ")"
		}
		fmt.Printf("%-12s %s  %s  %s%s\n", title, g.sourceLabel(f.seed.N), g.module(f.seed.N), g.frameLocation(f), in)
	}
	at("Crashed at:", c.crashedAt)
	at("Entry:", c.entry)
	if c.anchor >= 0 {
		f := c.frames[c.anchor]
		fmt.Printf("%-12s %s  (the own frame reaching the fewest)\n", "Screens of:", g.sourceLabel(f.seed.N))
	}
	for _, w := range c.warnings {
		fmt.Println("  warning: " + w)
	}
	if c.anchor >= 0 {
		g.screensText(c.impact, o)
		if a := c.impact; a.outScreens+a.outComps > 0 {
			fmt.Println("\n" + outsideLine(fmt.Sprintf("%d screens", a.outScreens), fmt.Sprintf("%d components", a.outComps)))
		}
	}
	own, other := 0, 0
	for _, f := range c.frames {
		if f.status == frameResolved || f.status == frameNotFound {
			own++
		} else {
			other++
		}
	}
	fmt.Printf("\nOwn frames: %d  (%d library and generated frames not shown)\n", own, other)
	for _, f := range c.frames {
		switch f.status {
		case frameResolved:
			fmt.Printf("  %-50s %d  %s\n", g.sourceLabel(f.seed.N), f.reach, g.frameLocation(f))
		case frameNotFound:
			fmt.Printf("  %-50s not in the code map\n", traceLine(f.Frame))
		}
	}
}

func (g *Graph) crashJSON(c *crashAnswer) {
	frame := func(f stack.Frame) obj {
		var line any
		if f.Line > 0 {
			line = f.Line
		}
		return obj{{"class", f.Class}, {"method", f.Method}, {"file", f.File}, {"line", line}}
	}
	var frames []any
	for _, f := range c.frames {
		x := append(obj{{"exception", f.exception}}, frame(f.Frame)...)
		var obf, sym, mod, loc any
		if f.Obfuscated != nil {
			obf = frame(*f.Obfuscated)
		}
		var reach any
		if f.status == frameResolved {
			sym, mod, loc, reach = g.qualified(f.seed.N), g.module(f.seed.N), g.frameLocation(f), f.reach
		}
		frames = append(frames, append(x, kv{"obfuscated", obf}, kv{"status", f.status}, kv{"symbol", sym},
			kv{"module", mod}, kv{"location", loc}, kv{"reach", reach}))
	}
	at := func(i int) any {
		if i < 0 {
			return nil
		}
		f := c.frames[i]
		var inKind, inName any
		if kind, name := g.frameIn(f); kind != "" {
			inKind, inName = kind, name
		}
		return obj{{"frame", i}, {"symbol", g.qualified(f.seed.N)}, {"module", g.module(f.seed.N)},
			{"location", g.frameLocation(f)}, {"in_kind", inKind}, {"in", inName}}
	}
	var chain []any
	for _, e := range c.trace.Exceptions {
		chain = append(chain, obj{{"class", e.Class}, {"message", e.Message}})
	}
	a := c.impact
	result := obj{
		{"exceptions", list(chain)},
		{"crashed_at", at(c.crashedAt)},
		{"entry", at(c.entry)},
		{"screens_of", at(c.anchor)},
		{"frames", list(frames)},
		{"screens", list(g.screensJSON(a, "crash"))},
		{"components", list(g.componentsJSON(a))},
		{"hidden", countsObj(a.hidden)},
		{"warnings", strs(c.warnings)},
	}
	if a.direct {
		result = append(result, kv{"hidden_by_via", countsObj(a.hiddenVia)})
	}
	if a.mf != nil {
		result = append(result, kv{"hidden_by_module", obj{{"screens", a.outScreens}, {"components", a.outComps}}})
	}
	g.printJSON("crash", result)
}
