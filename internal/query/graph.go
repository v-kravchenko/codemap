package query

// Queries over the code map snapshot: symbol lookup, direct users, labels.
// Nodes are node indexes (int32, -1: none); ids where the output or an order needs a stable number.

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/snapshot"
	"github.com/v-kravchenko/codemap/internal/util"
)

const (
	accPrivate = 0x0002
	accStatic  = 0x0008
)

var edgeKinds = map[int32]string{snapshot.CALL: "call", snapshot.READ: "read", snapshot.WRITE: "write", snapshot.TYPE: "type", snapshot.EXTENDS: "extends",
	snapshot.IMPLEMENTS: "implements", snapshot.LAMBDA: "lambda", snapshot.INLINE: "inline", snapshot.RES: "resource", snapshot.XML_RES: "xml",
	snapshot.XML_CLASS: "xml-class", snapshot.ROUTE_CONTENT: "route", snapshot.ROUTE_HOST: "host", snapshot.OPENS: "opens", snapshot.INJECT: "inject"}

var (
	screenClassKinds = util.Set("activity", "fragment", "dialog")
	screenKinds      = util.Set("activity", "fragment", "dialog", "route")
	diGlueRe         = regexp.MustCompile(`(_Factory|_Factory_Impl|_MembersInjector|_GeneratedInjector|_HiltModules\w*` +
		`|_ComponentTreeDeps|_HiltComponents|_Provide\w*Factory|_Binds\w*)(\$|$)|(^|/)Dagger|/hilt_aggregated_deps/|^dagger/|^hilt_aggregated_deps/`)
	diAnnotations = util.Set("dagger/Module", "dagger/hilt/InstallIn")
	symbolResRef  = regexp.MustCompile(`^(?:R\.|@)?(\w+)[./]([\w.]+)$`)
	resTypes      = util.Set("string", "color", "dimen", "style", "plurals", "array", "bool", "integer", "drawable",
		"mipmap", "layout", "navigation", "menu", "xml", "anim", "animator", "raw", "font", "transition",
		"interpolator", "fraction")
	generatedReaders = regexp.MustCompile(`^(equals|hashCode|toString|copy|copy\$default|component\d+|writeToParcel` +
		`|describeContents|write\$Self.*)$`)
	syntheticSuffix = regexp.MustCompile(`\$(?:suspendImpl|default|impl|p|\$forInline)$`)
	lambdaName      = regexp.MustCompile(`^(.*?)\$(?:lambda|\$inlined)\b`)
	fileLine        = regexp.MustCompile(`^(.*\.(?:kt|java|xml)):(\d+)$`)
	nodeOrder       = map[string]int{"C": 0, "F": 1, "M": 1, "R": 2, "ROUTE": 3}
)

type ScreenRow struct {
	Kind, Label, Target string
	Host                int32
}

type Graph struct {
	m                 *snapshot.Map
	root              string
	classByName       map[string]int32
	supRows           snapshot.CSR
	filePath          map[int32]string
	fileID            map[string]int32
	screens           map[int32][]ScreenRow
	screenOrder       []int32 // screen nodes in the order of their first row
	screenTops        map[int32]bool
	components        map[int32]string // class -> application, service, receiver, provider or worker
	pushComponents    map[int32]bool   // the components taking a push (nav_path.push_actions)
	routeOf           map[int32]int32
	routeContentNames map[string]int32
	drawn             map[int32][]int32         // route, preview -> the composables its content draws (ScreenContent)
	previews          map[int32][]int32         // screen -> the previews drawing its own composables (ScreenRelated)
	callVia           map[int32]map[int32]int32 // callee -> caller -> class the call names
	diGlue            map[int32]bool
	shown             []bool // by file id: the files of the nodes an answer names or walks

	supCache                   map[int32]map[int32]bool
	children                   map[int32][]int32
	subCounts                  map[int32]int
	viewmodels                 map[int32]bool
	routeHosts                 map[int32]map[int32]bool
	resDef                     map[int32]int // node -> its first res_def row
	sites                      map[int32][]int
	guards                     map[int32]map[[2]int32]map[[2]int32]bool
	routeContents, routeScopes map[int32]map[int32]bool // screenNames; scope -1: a method
	constCache                 map[int32][]int32
	srcFiles                   []string          // sourceFiles
	srcText                    map[string]string // sourceText
	holderCache                map[[2]int32][]int32
	labels                     []string // per node, "" not made yet (no label is empty: label() never returns "")
	qualifieds                 []string
	callers                    []int32 // callerClass per node, -2 not made yet
	pushes                     []int8  // pushWay per node: 0 not made yet, 1 no, 2 yes
	opened                     map[int32]openedRows
	entries                    map[int32][]navEntry
	refs                       any                     // meta.refs of the --json (changes)
	checked                    bool                    // whether the inputs were compared with the map's (not --no-check)
	stale                      *staleInfo              // nil: up to date or not checked
	uncompiled                 []string                // the modules whose class directories hold no class file now; nil: not checked
	resChanges                 map[int32][]resChange   // changes: the changed files of each resource seed
	removed                    []removedScreen         // changes: the screens it removes, nil when nothing tells
	base                       *Graph                  // changes: the map of the base (--base-db), nil without
	moves                      *moves                  // moved(), nil not made yet
	navHubs, navClasses        map[int32]bool          // isNavHub, nil not made yet
	refSups                    map[int32]map[int32]int // isRegistry: method -> repo supertype -> classes it names below it
}

func NewGraph(m *snapshot.Map, root string) *Graph {
	g := &Graph{m: m, root: root, classByName: m.ClassIndex(), supRows: m.SuperRows(),
		filePath: map[int32]string{}, fileID: map[string]int32{}, screens: map[int32][]ScreenRow{},
		screenTops: map[int32]bool{}, components: map[int32]string{}, routeOf: map[int32]int32{}, routeContentNames: map[string]int32{},
		callVia: map[int32]map[int32]int32{}, diGlue: map[int32]bool{},
		supCache: map[int32]map[int32]bool{}, viewmodels: map[int32]bool{}}
	n := m.Nodes()
	g.labels, g.qualifieds, g.callers = make([]string, n), make([]string, n), make([]int32, n)
	g.pushes = make([]int8, n)
	for i := range g.callers {
		g.callers[i] = -2
	}
	maxFile := int32(0)
	for _, id := range m.File.ID {
		maxFile = max(maxFile, id)
	}
	g.shown = make([]bool, maxFile+1)
	for i := range m.File.ID {
		p := m.File.Path.At(i)
		g.filePath[m.File.ID[i]] = p
		g.fileID[p] = m.File.ID[i]
	}
	s := &m.Screen
	for i := range s.Node {
		n := m.Index(s.Node[i])
		if _, ok := g.screens[n]; !ok {
			g.screenOrder = append(g.screenOrder, n)
		}
		g.screens[n] = append(g.screens[n], ScreenRow{s.Kind.At(i), s.Label.At(i), s.Target.At(i), m.Index(s.Host[i])})
	}
	g.pushComponents = map[int32]bool{}
	for i, c := range m.Component.Node {
		g.components[m.Index(c)] = m.Component.Kind.At(i)
		for a := range strings.SplitSeq(m.Component.Actions.At(i), ",") {
			if config.PushActions[a] {
				g.pushComponents[m.Index(c)] = true
			}
		}
	}
	g.drawn = map[int32][]int32{}
	for i := range m.ScreenContent.Screen {
		r := m.Index(m.ScreenContent.Screen[i])
		g.drawn[r] = append(g.drawn[r], m.Index(m.ScreenContent.Node[i]))
	}
	g.previews = map[int32][]int32{}
	for i := range m.ScreenRelated.Screen {
		p := m.Index(m.ScreenRelated.Related[i])
		if rows := g.screens[p]; len(rows) > 0 && rows[0].Kind == "preview" {
			s := m.Index(m.ScreenRelated.Screen[i])
			g.previews[s] = append(g.previews[s], p)
		}
	}
	for n, rows := range g.screens {
		if screenClassKinds[rows[0].Kind] {
			g.screenTops[n] = true
		}
	}
	var contents []int32
	for i := range m.Nodes() {
		for k := m.Out.Start[i]; k < m.Out.Start[i+1]; k++ {
			if m.Out.Kind[k] == snapshot.ROUTE_CONTENT {
				d := m.Out.Node[k]
				if _, ok := g.routeOf[d]; !ok {
					contents = append(contents, d)
				}
				g.routeOf[d] = int32(i)
			}
		}
	}
	for _, d := range contents {
		if g.kind(d) == "C" {
			g.routeContentNames[g.name(d)] = g.routeOf[d]
		}
	}
	v := &m.CallVia
	for i := range v.Src {
		if c, ok := g.classByName[v.Via.At(i)]; ok {
			dst := m.Index(v.Dst[i])
			if g.callVia[dst] == nil {
				g.callVia[dst] = map[int32]int32{}
			}
			g.callVia[dst][m.Index(v.Src[i])] = c
		}
	}
	for name, c := range g.classByName {
		if isDIGlue(name) {
			g.diGlue[c] = true
		}
	}
	for i := range m.Annotation.Node {
		if diAnnotations[m.Annotation.Name.At(i)] {
			g.diGlue[m.Index(m.Annotation.Node[i])] = true
		}
	}
	return g
}

// isDIGlue: diGlueRe.MatchString without the regexp (RE2 backtracking costs 70 ms per query). A \w
// run cannot hold `$`, so each token must end its run, right before `$` or the end.
func isDIGlue(name string) bool {
	if strings.HasPrefix(name[strings.LastIndexByte(name, '/')+1:], "Dagger") || strings.HasPrefix(name, "dagger/") ||
		strings.HasPrefix(name, "hilt_aggregated_deps/") || strings.Contains(name, "/hilt_aggregated_deps/") {
		return true
	}
	for i := strings.IndexByte(name, '_'); i >= 0; {
		j := i + 1 // the end of the \w run from i
		for j < len(name) && isWord(name[j]) {
			j++
		}
		if j == len(name) || name[j] == '$' {
			for k := i; k < j; k++ {
				if name[k] != '_' {
					continue
				}
				run := name[k:j]
				switch {
				case run == "_Factory", run == "_Factory_Impl", run == "_MembersInjector", run == "_GeneratedInjector",
					run == "_ComponentTreeDeps", run == "_HiltComponents", strings.HasPrefix(run, "_HiltModules"),
					strings.HasPrefix(run, "_Binds"),
					strings.HasPrefix(run, "_Provide") && strings.HasSuffix(run[len("_Provide"):], "Factory"):
					return true
				}
			}
		}
		next := strings.IndexByte(name[j:], '_')
		if next < 0 {
			break
		}
		i = j + next
	}
	return false
}

func isWord(c byte) bool {
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// ------------------------------------------------------------ nodes

func (g *Graph) id(n int32) int64     { return g.m.Node.ID[n] }
func (g *Graph) kind(n int32) string  { return g.m.Node.Kind.At(int(n)) }
func (g *Graph) name(n int32) string  { return g.m.Node.Name.At(int(n)) }
func (g *Graph) desc(n int32) string  { return g.m.Node.Desc.At(int(n)) }
func (g *Graph) sname(n int32) string { return g.m.Node.Sname.At(int(n)) }
func (g *Graph) flags(n int32) int32  { return g.m.Node.Flags[n] }
func (g *Graph) owner(n int32) int32  { return g.m.Node.OwnerIx[n] }
func (g *Graph) fileOf(n int32) int32 { return g.m.Node.File[n] }
func (g *Graph) members(c int32) []int32 {
	return g.m.Members.Node[g.m.Members.Start[c]:g.m.Members.Start[c+1]]
}
func (g *Graph) show(n int32) { g.shown[g.fileOf(n)] = true }

func (g *Graph) isMember(n int32) bool {
	k := g.kind(n)
	return k == "C" || k == "M" || k == "F"
}

func dottedShort(name string) string {
	name = name[strings.LastIndex(name, "/")+1:]
	return strings.ReplaceAll(strings.ReplaceAll(name, "$$", "$"), "$", ".")
}

func afterDot(s string) string {
	_, after, _ := strings.Cut(s, ".")
	return after
}

func lastSegment(name string) string { return name[strings.LastIndex(name, "/")+1:] }

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// label: the source-level name; a lambda, coroutine or inlined body reads as `Owner.method{λ}`.
func (g *Graph) label(n int32) string {
	g.show(n)
	if g.labels[n] == "" {
		g.labels[n] = g.makeLabel(n)
	}
	return g.labels[n]
}

func (g *Graph) makeLabel(n int32) string {
	switch g.kind(n) {
	case "C":
		name := g.name(n)
		if strings.HasPrefix(lastSegment(name), "ComposableSingletons$") {
			return afterDot(dottedShort(name)) + "{λ}"
		}
		outer, method, ok := g.enclosing(name)
		if !ok {
			return dottedShort(outer)
		}
		if method != "" {
			return dottedShort(outer) + "." + method + "{λ}"
		}
		return dottedShort(outer) + "{λ}"
	case "M", "F":
		outer, method, ok := g.enclosing(g.name(g.owner(n)))
		if ok {
			if method != "" {
				return dottedShort(outer) + "." + method + "{λ}"
			}
			return dottedShort(outer) + "{λ}"
		}
		if strings.HasPrefix(lastSegment(outer), "ComposableSingletons$") {
			return afterDot(dottedShort(outer)) + "{λ}"
		}
		name := syntheticSuffix.ReplaceAllString(strings.TrimPrefix(g.sname(n), "access$"), "")
		if mm := lambdaName.FindStringSubmatch(name); mm != nil {
			return dottedShort(outer) + "." + strings.ReplaceAll(mm[1], "$", ".") + "{λ}"
		}
		if len(name) > 1 {
			name = name[:1] + strings.ReplaceAll(name[1:], "$", ".")
		}
		if g.kind(n) == "M" {
			name += "()"
		}
		return dottedShort(outer) + "." + name
	case "R":
		return "@" + g.name(n)
	case "ROUTE":
		return "route " + g.sname(n)
	}
	return g.name(n)
}

// enclosing: (class, method, true) for a compiler-made class (`A$load$1`, `A$load$$inlined$map$1`):
// the source class and the method whose body holds it; (name, "", false) for a source class.
func (g *Graph) enclosing(name string) (string, string, bool) {
	parts := strings.Split(name, "$")
	cls, i := parts[0], 1
	for i < len(parts) {
		part := parts[i]
		if part == "" && i+1 < len(parts) && parts[i+1] != "inlined" && parts[i+1] != "lambda" {
			part, i = "$"+parts[i+1], i+1 // `$$serializer`
		}
		if part != "" && !isDigits(part) && part != "lambda" {
			if _, ok := g.classByName[cls+"$"+part]; ok {
				cls += "$" + part
				i++
				continue
			}
		}
		if cls == name || i == len(parts)-1 && part != "" && !isDigits(part) {
			return name, "", false
		}
		method := ""
		if part != "" && !isDigits(part) && part != "lambda" {
			method = part
		}
		if method == "special" { // property initializers
			method = "init"
		}
		return cls, method, true
	}
	return cls, "", false
}

func (g *Graph) location(n int32) string {
	g.show(n)
	path, ok := g.filePath[g.fileOf(n)]
	if !ok || path == "" {
		return ""
	}
	line := g.m.Node.LMin[n]
	if g.kind(n) == "R" {
		if r, ok := g.resDefRow(n); ok {
			path, line = g.filePath[g.m.ResDef.File[r]], g.m.ResDef.Line[r]
		}
	} else if g.kind(n) == "C" && line == 0 {
		for _, c := range g.members(n) {
			if l := g.m.Node.LMin[c]; l != 0 && (line == 0 || l < line) {
				line = l
			}
		}
	}
	if line != 0 {
		return path + ":" + strconv.Itoa(int(line))
	}
	return path
}

func (g *Graph) resDefRow(n int32) (int, bool) {
	if g.resDef == nil {
		g.resDef = map[int32]int{}
		for r := len(g.m.ResDef.Node) - 1; r >= 0; r-- {
			g.resDef[g.m.Index(g.m.ResDef.Node[r])] = r
		}
	}
	r, ok := g.resDef[n]
	return r, ok
}

func (g *Graph) module(n int32) string {
	if id := int(g.m.Node.Module[n]); id >= 1 && id <= g.m.Module.Name.Len() {
		return g.m.Module.Name.At(id - 1)
	}
	return "?"
}

func (g *Graph) topOf(n int32) int32 {
	if g.isMember(n) {
		return g.m.Node.TopIx[n]
	}
	return n
}

// ------------------------------------------------------------ hierarchy

func (g *Graph) supNames(c int32) []int32 {
	if c < 0 {
		return nil
	}
	return g.supRows.Node[g.supRows.Start[c]:g.supRows.Start[c+1]]
}

func (g *Graph) supertypes(c int32) []int32 {
	if c < 0 {
		return nil
	}
	return g.m.Supertypes(g.supRows, c)
}

func (g *Graph) supSet(c int32) map[int32]bool {
	s, ok := g.supCache[c]
	if !ok {
		s = map[int32]bool{}
		for _, x := range g.supertypes(c) {
			s[x] = true
		}
		g.supCache[c] = s
	}
	return s
}

// isSubtype: whether `sup` is among the repo supertypes of `c`.
func (g *Graph) isSubtype(c, sup int32) bool { return c >= 0 && g.supSet(c)[sup] }

func (g *Graph) kin(a, b int32) bool { return a == b || g.supSet(b)[a] || g.supSet(a)[b] }

func (g *Graph) routeContaining(n int32) int32 {
	if r, ok := g.routeOf[n]; ok {
		return r
	}
	if k := g.kind(n); k == "M" || k == "F" {
		n = g.owner(n)
		if n < 0 {
			return -1
		}
	}
	if g.kind(n) != "C" || n == g.m.Node.TopIx[n] {
		return -1
	}
	name := g.name(n)
	for strings.Contains(name, "$") {
		if r, ok := g.routeContentNames[name]; ok {
			return r
		}
		name = name[:strings.LastIndex(name, "$")]
	}
	return -1
}

// creators: methods outside the class that use its type (create it, take or return it), and whether
// DI glue is among them.
func (g *Graph) creators(c int32) ([]int32, bool) {
	var out []int32
	di := false
	own := g.name(c)
	users := g.users(c, snapshot.TYPE)
	for _, m := range g.members(c) {
		if nm := g.name(m); nm == "<init>" || nm == "INSTANCE" {
			users = append(users, g.users(m, snapshot.CALL, snapshot.READ)...)
		}
	}
	for _, u := range users {
		if g.diGlue[g.topOf(u.N)] {
			di = true
		} else if cls := g.classNameOf(u.N); cls != own && !strings.HasPrefix(cls, own+"$") {
			out = append(out, u.N)
		}
	}
	return out, di
}

// holders: the classes that get an instance of `c` from DI: with a field of its type or of a generic
// type `S<A>` it implements (`S` the class `sup`), or calling a method that returns it (an
// `@AssistedFactory`). Nil: none, the instance can be anywhere.
func (g *Graph) holders(c, sup int32) []int32 {
	if out, ok := g.holderCache[[2]int32{c, sup}]; ok {
		return out
	}
	if g.holderCache == nil {
		g.holderCache = map[[2]int32][]int32{}
	}
	var out []int32
	add := func(h int32) {
		if h >= 0 && g.topOf(h) != g.topOf(c) && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	returns := ")L" + g.name(c) + ";"
	for _, u := range g.users(c, snapshot.TYPE, snapshot.INJECT) {
		switch {
		case g.topOf(u.N) == g.topOf(c):
		case g.kind(u.N) == "M" && u.Kind == snapshot.TYPE && strings.HasSuffix(g.desc(u.N), returns):
			for _, call := range g.users(u.N, snapshot.CALL) { // an @AssistedFactory, also inside a @Module
				if !g.diGlue[g.topOf(call.N)] {
					add(g.callerClass(call.N))
				}
			}
		case g.diGlue[g.topOf(u.N)]:
		case g.kind(u.N) != "F" || strings.HasPrefix(g.name(u.N), "$") || strings.HasPrefix(g.name(u.N), "this$"):
			// a lambda's capture
		case u.Kind == snapshot.TYPE || slices.ContainsFunc(g.uses(u.N, snapshot.TYPE), func(t Use) bool { return t.N == sup }):
			add(g.owner(u.N))
		}
	}
	g.holderCache[[2]int32{c, sup}] = out
	return out
}

// routeHostsOf: the screen classes whose code registers the content of `route`.
func (g *Graph) routeHostsOf(route int32) map[int32]bool {
	if g.routeHosts == nil {
		g.routeHosts = map[int32]map[int32]bool{}
		for content, r := range g.routeOf {
			if g.routeHosts[r] == nil {
				g.routeHosts[r] = map[int32]bool{}
			}
			// from the registering code too: build moved its edge to a content lambda onto the route
			starts := []int32{content}
			for _, u := range g.uses(r, snapshot.ROUTE_HOST) {
				starts = append(starts, u.N)
			}
			for _, n := range starts {
				for s := range g.registeringScreens(n) {
					g.routeHosts[r][s] = true
				}
			}
		}
	}
	return g.routeHosts[route]
}

func (g *Graph) registeringScreens(n int32) map[int32]bool {
	out, seen, stack := map[int32]bool{}, map[int32]bool{}, []int32{n}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[n] {
			continue
		}
		seen[n] = true
		top := g.topOf(n)
		if g.screenTops[top] {
			out[top] = true
			continue
		}
		if g.diGlue[top] {
			continue
		}
		for _, u := range g.users(n, snapshot.CALL, snapshot.LAMBDA, snapshot.INLINE) {
			stack = append(stack, u.N)
		}
		if g.kind(n) == "M" {
			// a lambda's method is run by whoever makes its class: `navigation(R) { section() }`
			if o := g.owner(n); o >= 0 {
				if _, _, ok := g.enclosing(g.name(o)); ok {
					stack = append(stack, o)
				}
			}
		}
		if g.kind(n) == "C" {
			cls, method, ok := g.enclosing(g.name(n))
			if c, found := g.classByName[cls]; ok && method != "" && found {
				for _, m := range g.members(c) {
					if g.kind(m) == "M" && g.name(m) == method {
						stack = append(stack, m)
					}
				}
			}
		}
	}
	return out
}

// hosts: the screens hosting `sid`, by label.
func (g *Graph) hosts(sid int32) []int32 {
	var out []int32
	if g.screens[sid][0].Kind == "route" {
		for h := range g.routeHostsOf(sid) {
			out = append(out, h)
		}
		slices.Sort(out)
	}
	if len(out) == 0 && g.screens[sid][0].Host >= 0 {
		out = []int32{g.screens[sid][0].Host}
	}
	sort.SliceStable(out, func(i, j int) bool { return g.label(out[i]) < g.label(out[j]) })
	return out
}

func (g *Graph) isViewModel(c int32) bool {
	v, ok := g.viewmodels[c]
	if !ok {
		for _, x := range append([]int32{c}, g.supertypes(c)...) {
			for _, r := range g.supNames(x) {
				if g.m.Hierarchy.Sup.At(int(r)) == "androidx/lifecycle/ViewModel" {
					v = true
				}
			}
		}
		g.viewmodels[c] = v
	}
	return v
}

func (g *Graph) subclassCount(c int32) int {
	if g.children == nil {
		g.children, g.subCounts = map[int32][]int32{}, map[int32]int{}
		h := &g.m.Hierarchy
		for r := range h.ClsIx {
			if h.SupIx[r] >= 0 {
				g.children[h.SupIx[r]] = append(g.children[h.SupIx[r]], h.ClsIx[r])
			}
		}
	}
	if n, ok := g.subCounts[c]; ok {
		return n
	}
	seen, stack := map[int32]bool{}, slices.Clone(g.children[c])
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !seen[x] {
			seen[x] = true
			stack = append(stack, g.children[x]...)
		}
	}
	g.subCounts[c] = len(seen)
	return len(seen)
}

func (g *Graph) classNameOf(n int32) string {
	if g.kind(n) == "C" {
		return g.name(n)
	}
	if o := g.owner(n); o >= 0 {
		return g.name(o)
	}
	return ""
}

func (g *Graph) hasExternalAncestor(c int32) bool {
	stack, seen := []int32{c}, map[int32]bool{}
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[x] {
			continue
		}
		seen[x] = true
		for _, r := range g.supNames(x) {
			if s := g.m.Hierarchy.SupIx[r]; s < 0 {
				if g.m.Hierarchy.Sup.At(int(r)) != "java/lang/Object" {
					return true
				}
			} else {
				stack = append(stack, s)
			}
		}
	}
	return false
}

// ------------------------------------------------------------ edges

type Use struct {
	N    int32
	Kind int32
}

// order: edges by kind, then by the content of the other node (not ids): the walk's choices between
// ways of one length do not change when ids do.
func (g *Graph) order(rows []Use) []Use {
	type sk struct {
		o               int
		cls, name, desc string
	}
	keys := make([]sk, len(rows))
	for i, r := range rows {
		o, ok := nodeOrder[g.kind(r.N)]
		if !ok {
			o = 9
		}
		keys[i] = sk{o, g.classNameOf(r.N), g.name(r.N), g.desc(r.N)}
	}
	idx := make([]int, len(rows))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		x, y := idx[a], idx[b]
		if rows[x].Kind != rows[y].Kind {
			return rows[x].Kind < rows[y].Kind
		}
		kx, ky := keys[x], keys[y]
		if kx.o != ky.o {
			return kx.o < ky.o
		}
		if kx.cls != ky.cls {
			return kx.cls < ky.cls
		}
		if kx.name != ky.name {
			return kx.name < ky.name
		}
		return kx.desc < ky.desc
	})
	out := make([]Use, len(rows))
	for i, x := range idx {
		out[i] = rows[x]
	}
	return out
}

func edgesOf(c *snapshot.CSR, n int32, kinds []int32) []Use {
	var out []Use
	for k := c.Start[n]; k < c.Start[n+1]; k++ {
		if len(kinds) == 0 || slices.Contains(kinds, c.Kind[k]) {
			out = append(out, Use{c.Node[k], c.Kind[k]})
		}
	}
	return out
}

func (g *Graph) users(n int32, kinds ...int32) []Use { return g.order(edgesOf(&g.m.In, n, kinds)) }
func (g *Graph) uses(n int32, kinds ...int32) []Use  { return g.order(edgesOf(&g.m.Out, n, kinds)) }

// reaches: whether the call of `callee` in `src` can run on an instance of class `receiver` (-1: any).
func (g *Graph) reaches(src, callee, receiver int32) bool {
	if receiver < 0 {
		return true
	}
	if via, ok := g.callVia[callee][src]; ok && !g.kin(via, receiver) {
		return false
	}
	caller := g.callerClass(src)
	owner := g.owner(callee)
	if caller < 0 || caller == owner || !g.isSubtype(caller, owner) {
		return true
	}
	return g.kin(caller, receiver)
}

// callerCtx: `receiver` stays known in a caller of the same class hierarchy (a `this` call).
func (g *Graph) callerCtx(src, receiver int32) int32 {
	caller := g.callerClass(src)
	if receiver < 0 || caller < 0 {
		return -1
	}
	if receiver == caller || g.supSet(receiver)[caller] {
		return receiver
	}
	return -1
}

func (g *Graph) generatedRead(src, field int32) bool {
	if g.kind(src) != "M" {
		return false
	}
	if generatedReaders.MatchString(g.name(src)) {
		return true
	}
	owner := g.name(g.owner(src))
	if strings.HasSuffix(owner, "$$serializer") || strings.HasSuffix(owner, "$Creator") {
		return true
	}
	return g.owner(src) == g.owner(field) && (g.name(src) == "<clinit>" || g.name(src) == "$values")
}

func (g *Graph) callerClass(src int32) int32 {
	if g.callers[src] == -2 {
		g.callers[src] = -1
		cls, _, _ := g.enclosing(g.classNameOf(src))
		if c, ok := g.classByName[cls]; ok {
			g.callers[src] = c
		}
	}
	return g.callers[src]
}

// overridden: the same method declared in repo supertypes of its class.
func (g *Graph) overridden(m int32) []int32 {
	var out []int32
	if nm := g.name(m); nm == "<init>" || nm == "<clinit>" || g.flags(m)&(accStatic|accPrivate) != 0 {
		return out
	}
	name, desc := g.name(m), g.desc(m)
	for _, s := range g.supertypes(g.owner(m)) {
		for _, x := range g.members(s) {
			if g.name(x) == name && g.desc(x) == desc && desc != "" {
				out = append(out, x)
				break
			}
		}
	}
	return out
}

func (g *Graph) overriders(m int32) []int32 {
	return g.m.Overriders.Node[g.m.Overriders.Start[m]:g.m.Overriders.Start[m+1]]
}

// ------------------------------------------------------------ resolution

type Seed struct {
	N    int32
	Mode byte // modeMember, modeWhole
}

func sortSeeds(s []Seed) []Seed {
	seen := map[Seed]bool{}
	var out []Seed
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].N != out[j].N {
			return out[i].N < out[j].N
		}
		return modeName[out[i].Mode] < modeName[out[j].Mode]
	})
	return out
}

func (g *Graph) resolve(text string) []Seed {
	text = strings.TrimSpace(text)
	if mm := symbolResRef.FindStringSubmatch(text); mm != nil && resTypes[mm[1]] &&
		(strings.HasPrefix(text, "R.") || strings.HasPrefix(text, "@") || strings.Contains(text, "/")) {
		key := mm[1] + "/" + strings.ReplaceAll(mm[2], ".", "_")
		for i := range g.m.Nodes() {
			if g.kind(int32(i)) == "R" && g.name(int32(i)) == key {
				return []Seed{{int32(i), modeWhole}}
			}
		}
		return nil
	}
	path, line := text, 0
	if pm := fileLine.FindStringSubmatch(text); pm != nil {
		path = pm[1]
		line, _ = strconv.Atoi(pm[2])
	}
	rel := path
	if _, err := os.Stat(path); err == nil {
		abs, _ := filepath.Abs(path)
		rel, _ = filepath.Rel(g.root, abs)
	}
	rel = filepath.ToSlash(rel) // the map's paths use /
	if _, ok := g.fileID[rel]; ok {
		var lines []int32
		if line != 0 {
			lines = []int32{int32(line)}
		}
		return g.resolveFile(rel, lines)
	}
	return g.resolveSymbol(text)
}

func (g *Graph) resolveFile(rel string, lines []int32) []Seed {
	fid, ok := g.fileID[rel]
	if !ok {
		return nil
	}
	if strings.HasSuffix(rel, ".xml") {
		var all, hit []Seed
		anyLine := false
		r := &g.m.ResDef
		for i := range r.Node {
			if r.File[i] != fid {
				continue
			}
			n := g.m.Index(r.Node[i])
			all = append(all, Seed{n, modeWhole})
			if r.Line[i] != 0 {
				anyLine = true
			}
			if slices.Contains(lines, r.Line[i]) {
				hit = append(hit, Seed{n, modeWhole})
			}
		}
		if len(lines) > 0 && anyLine && len(hit) > 0 {
			return hit
		}
		return all
	}
	nd := &g.m.Node
	if len(lines) > 0 {
		var out []Seed
		for _, ln := range lines {
			best := int32(-1)
			for i := range g.m.Nodes() {
				if nd.File[i] == fid && nd.LMin[i] != 0 && nd.LMax[i] != 0 && nd.LMin[i] <= ln && nd.LMax[i] >= ln &&
					g.kind(int32(i)) == "M" && (best < 0 || nd.LMax[i]-nd.LMin[i] < nd.LMax[best]-nd.LMin[best]) {
					best = int32(i)
				}
			}
			if best >= 0 {
				out = append(out, Seed{best, modeMember})
			}
		}
		if len(out) > 0 {
			return sortSeeds(out)
		}
	}
	var out []Seed
	for i := range g.m.Nodes() {
		if nd.File[i] == fid && nd.TopIx[i] == int32(i) && g.kind(int32(i)) == "C" {
			out = append(out, Seed{int32(i), modeWhole})
		}
	}
	return out
}

func (g *Graph) resolveClass(text string) []int32 {
	q := strings.ReplaceAll(strings.ReplaceAll(text, "$", "."), "/", ".")
	var exact, suffix []int32
	dot := func(c byte) byte {
		if c == '/' || c == '$' {
			return '.'
		}
		return c
	}
	for name, c := range g.classByName { // dotted(name) == q, or ends with "." + q, without building it
		n, m := len(name), len(q)
		if n < m {
			continue
		}
		same := true
		for i := 1; i <= m && same; i++ {
			same = dot(name[n-i]) == q[m-i]
		}
		if !same {
			continue
		}
		if n == m {
			exact = append(exact, c)
		} else if dot(name[n-m-1]) == '.' {
			suffix = append(suffix, c)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return suffix
}

func capitalized(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (g *Graph) resolveSymbol(text string) []Seed {
	text = strings.TrimRight(strings.ReplaceAll(text, "#", "."), "()")
	var found []Seed
	for _, c := range g.resolveClass(text) {
		found = append(found, Seed{c, modeWhole})
	}
	if i := strings.LastIndex(text, "."); i >= 0 {
		for _, c := range g.resolveClass(text[:i]) {
			for _, n := range g.membersNamed(c, text[i+1:]) {
				found = append(found, Seed{n, modeMember})
			}
		}
	}
	if len(found) == 0 { // top-level function or property: pkg.fn / fn
		pkg, member := "", text
		if i := strings.LastIndex(text, "."); i >= 0 {
			pkg, member = text[:i], text[i+1:]
		}
		getter := "get" + capitalized(member)
		for i := range g.m.Nodes() {
			n := int32(i)
			if k := g.kind(n); k != "M" && k != "F" {
				continue
			}
			if s := g.sname(n); s != member && s != getter {
				continue
			}
			o := g.owner(n)
			if o < 0 {
				continue
			}
			cname := g.name(o)
			if !strings.HasSuffix(strings.ToLower(cname), "kt") {
				continue
			}
			p := cname
			if j := strings.LastIndex(cname, "/"); j >= 0 {
				p = cname[:j]
			}
			if pkg == "" || strings.ReplaceAll(p, "/", ".") == pkg {
				found = append(found, Seed{n, modeMember})
			}
		}
	}
	return sortSeeds(found)
}

func (g *Graph) membersNamed(c int32, member string) []int32 {
	getter, setter := "get"+capitalized(member), "set"+capitalized(member)
	var out []int32
	for _, m := range g.members(c) {
		s := g.sname(m)
		if s == member || s == getter || s == setter || g.kind(m) == "F" && g.name(m) == member {
			out = append(out, m)
		}
	}
	return out
}

// ------------------------------------------------------------ direct users

type EdgeRow struct {
	Other int32
	Kind  string
	Via   int32
}

// directUsers: (user, edge kind, used node) of a node; for a whole class, of all its members.
func (g *Graph) directUsers(n int32, mode byte) []EdgeRow {
	targets := []int32{n}
	k := g.kind(n)
	if k == "C" && mode == modeWhole {
		targets = append(targets, g.members(n)...)
	}
	if k == "M" {
		targets = append(targets, g.overridden(n)...)
	}
	var out []EdgeRow
	for _, t := range targets {
		in := edgesOf(&g.m.In, t, nil)
		sort.SliceStable(in, func(i, j int) bool { return in[i].Kind < in[j].Kind }) // edge_dst(dst, kind) order
		for _, u := range in {
			if u.Kind == snapshot.ROUTE_HOST || u.Kind == snapshot.INJECT || k == "C" && g.topOf(u.N) == g.m.Node.TopIx[n] {
				continue
			}
			out = append(out, EdgeRow{u.N, edgeKinds[u.Kind], t})
		}
	}
	if k == "F" && g.flags(n)&snapshot.ConstantFlag != 0 {
		for _, u := range g.constUsers(n) {
			out = append(out, EdgeRow{u, "source", n})
		}
	}
	return out
}

// constUsers: methods whose source mentions a constant (Kotlin inlines `const val`): files with the
// name and an import of it, of its class or package, or in the same package.
func (g *Graph) constUsers(field int32) []int32 {
	if u, ok := g.constCache[field]; ok {
		return u
	}
	u := g.findConstUsers(field)
	if g.constCache == nil {
		g.constCache = map[int32][]int32{}
	}
	g.constCache[field] = u
	return u
}

func (g *Graph) findConstUsers(field int32) []int32 {
	owner := g.name(g.owner(field))
	pkg, cls := owner, owner
	if i := strings.LastIndex(owner, "/"); i >= 0 {
		pkg, cls = strings.ReplaceAll(owner[:i], "/", "."), owner[i+1:]
	}
	cls, _, _ = strings.Cut(cls, "$")
	name := g.name(field)
	var paths []string
	hits := map[string][]int32{}
	for _, p := range g.sourceFiles() {
		if lines := wordLines(g.sourceText(p), name); len(lines) > 0 {
			paths = append(paths, p)
			hits[p] = lines
		}
	}
	samePkg := regexp.MustCompile(`(?m)^package\s+` + regexp.QuoteMeta(pkg) + `\s*$`)
	imports := []string{pkg + "." + name, pkg + "." + cls, pkg + ".*"}
	seen := map[int32]bool{}
	var users []int32
	for _, p := range paths {
		text := g.sourceText(p)
		ok := samePkg.MatchString(text)
		for _, i := range imports {
			ok = ok || strings.Contains(text, "import "+i)
		}
		if !ok {
			continue
		}
		for _, s := range g.resolveFile(p, hits[p]) {
			if s.N != field && g.kind(s.N) == "M" && !seen[s.N] {
				seen[s.N] = true
				users = append(users, s.N)
			}
		}
	}
	slices.Sort(users)
	return users
}

// sourceFiles: the Kotlin and Java sources of the map outside the debug source sets, sorted.
func (g *Graph) sourceFiles() []string {
	if g.srcFiles == nil {
		g.srcFiles = []string{}
		for _, p := range g.filePath {
			if (strings.HasSuffix(p, ".kt") || strings.HasSuffix(p, ".java")) &&
				!slices.ContainsFunc(config.Cfg.Screens.DebugSourceSets, func(s string) bool { return strings.Contains(p, "/src/"+s+"/") }) {
				g.srcFiles = append(g.srcFiles, p)
			}
		}
		slices.Sort(g.srcFiles)
	}
	return g.srcFiles
}

// sourceText: the text of a source file, read once ("" if unreadable).
func (g *Graph) sourceText(rel string) string {
	if t, ok := g.srcText[rel]; ok {
		return t
	}
	if g.srcText == nil {
		g.srcText = map[string]string{}
	}
	data, _ := os.ReadFile(filepath.Join(g.root, rel))
	g.srcText[rel] = string(data)
	return g.srcText[rel]
}

// wordLines: the 1-based lines where word occurs as a whole word (letters, digits, '_').
func wordLines(text, word string) []int32 {
	var out []int32
	line, from := int32(1), 0
	for {
		i := strings.Index(text[from:], word)
		if i < 0 {
			return out
		}
		i += from
		line += int32(strings.Count(text[from:i], "\n"))
		end := i + len(word)
		if (i == 0 || !isWordByte(text[i-1])) && (end == len(text) || !isWordByte(text[end])) &&
			(len(out) == 0 || out[len(out)-1] != line) {
			out = append(out, line)
		}
		from = i + 1
		line += int32(strings.Count(text[i:from], "\n"))
	}
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}
