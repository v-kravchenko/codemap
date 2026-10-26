package query

// The query commands: used-by, uses, impact, find, screens, build --info; text or --json.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime/debug"
	"runtime/pprof"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/snapshot"
	"github.com/v-kravchenko/codemap/internal/util"
)

const (
	toolVersion   = "0.2.0"
	schemaVersion = "3.4.0"
	exitOK        = 0
	exitError     = 2
)

var (
	kindNames      = map[string]string{"C": "class", "M": "method", "F": "field", "R": "resource", "ROUTE": "route"}
	findTypes      = []string{"class", "method", "field", "resource"} // find --type: find lists no routes
	syntheticClass = regexp.MustCompile(`\$(\d|\$|DefaultImpls$|WhenMappings$)|_HiltModules_\w+$`)
	started        = time.Now()
)

type opts struct {
	db, to, base, head, diff, files, from, entry, direction, host string
	json, all, noPaths, noSelf, noDI, noCheck, nav                bool
	strict, verbose, info, ways, in, unreachable                  bool
	depth, limit                                                  int
	kinds, pos, via, avoid, modules, types                        []string
}

func parseOpts(cmd string, args []string) opts {
	o := opts{depth: -1, limit: -1}
	spec := commandSpec(cmd)
	// inline: the value of `--flag=value`, taken by the flag instead of the next argument
	inline, hasInline := "", false
	value := func(i *int, name string) string {
		if hasInline {
			hasInline = false
			return inline
		}
		if *i+1 >= len(args) {
			panic(util.UserError("argument --" + name + ": expected one argument"))
		}
		*i++
		return args[*i]
	}
	number := func(i *int, name string) int {
		v, err := strconv.Atoi(value(i, name))
		if err != nil {
			panic(util.UserError("argument --" + name + ": invalid int value"))
		}
		if v < 0 {
			panic(util.UserError("argument --" + name + ": expected N >= 0"))
		}
		return v
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		hasInline = false
		if name, v, ok := strings.Cut(a, "="); ok && strings.HasPrefix(a, "--") {
			a, inline, hasInline = name, v, true
		}
		if strings.HasPrefix(a, "--") && !spec.hasFlag(a[2:]) {
			if knownFlag(a[2:]) {
				panic(util.UserError(fmt.Sprintf("argument %s: not an option of %s (codemap %s --help)", a, spec.name, cmd)))
			}
			panic(util.UserError("unrecognized arguments: " + a))
		}
		switch a {
		case "--db":
			o.db = value(&i, "db")
		case "--to":
			o.to = value(&i, "to")
		case "--base":
			o.base = value(&i, "base")
		case "--head":
			o.head = value(&i, "head")
		case "--diff":
			o.diff = value(&i, "diff")
		case "--files":
			o.files = value(&i, "files")
		case "--verbose":
			o.verbose = true
		case "--info":
			o.info = true
		case "--depth":
			o.depth = number(&i, "depth")
		case "--limit":
			o.limit = number(&i, "limit")
		case "--kind":
			o.kinds = append(o.kinds, value(&i, "kind"))
		case "--json":
			o.json = true
		case "--all":
			o.all = true
		case "--no-paths":
			o.noPaths = true
		case "--no-self":
			o.noSelf = true
		case "--no-di":
			o.noDI = true
		case "--host":
			o.host = value(&i, "host")
		case "--unreachable":
			o.unreachable = true
		case "--type":
			o.types = append(o.types, value(&i, "type"))
		case "--no-check":
			o.noCheck = true
		case "--strict":
			o.strict = true
		case "--nav":
			o.nav = true
		case "--via":
			o.via = append(o.via, value(&i, "via"))
		case "--avoid":
			o.avoid = append(o.avoid, value(&i, "avoid"))
		case "--from":
			o.from = value(&i, "from")
		case "--entry":
			o.entry = value(&i, "entry")
		case "--ways":
			o.ways = true
		case "--in":
			o.in = true
		case "--direction":
			o.direction = strings.ToUpper(value(&i, "direction"))
		case "--module":
			o.modules = append(o.modules, value(&i, "module"))
		default:
			if strings.HasPrefix(a, "--") {
				panic(util.UserError("unrecognized arguments: " + a))
			}
			o.pos = append(o.pos, a)
		}
		if hasInline {
			panic(util.UserError(fmt.Sprintf("argument %s: takes no value, got '%s'", a, inline)))
		}
	}
	if n := spec.maxArgs(); n >= 0 && len(o.pos) > n {
		panic(util.UserError("unrecognized arguments: " + strings.Join(o.pos[n:], " ")))
	}
	if o.direction != "" && !slices.Contains(mermaidDirections, o.direction) {
		panic(util.UserError(fmt.Sprintf("argument --direction: invalid choice: '%s' (choose from %s)", o.direction,
			strings.Join(mermaidDirections, ", "))))
	}
	if o.entry != "" && !slices.Contains(entryKinds, o.entry) {
		panic(util.UserError(fmt.Sprintf("argument --entry: invalid choice: '%s' (choose from %s)", o.entry,
			strings.Join(entryKinds, ", "))))
	}
	for _, t := range o.types {
		if !slices.Contains(findTypes, t) {
			panic(util.UserError(fmt.Sprintf("argument --type: invalid choice: '%s' (choose from %s)", t,
				strings.Join(findTypes, ", "))))
		}
	}
	for _, k := range o.kinds {
		if k != "all" && !slices.ContainsFunc(config.ScreenTitles, func(t [2]string) bool { return t[0] == k }) {
			panic(util.UserError(fmt.Sprintf("argument --kind: invalid choice: '%s'", k)))
		}
	}
	return o
}

func Query(cmd string, args []string) (code int) {
	defer func() {
		if r := recover(); r != nil {
			msg, ok := r.(util.UserError)
			if !ok {
				panic(r)
			}
			fmt.Fprintln(os.Stderr, "error: "+string(msg))
			code = exitError
		}
	}()
	if pf := os.Getenv("CODEMAP_PROFILE"); pf != "" {
		f, _ := os.Create(pf)
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	debug.SetGCPercent(400)
	o := parseOpts(cmd, args)
	root := util.FindRoot()
	if o.db == "" {
		o.db = config.DefaultSnapshot(root)
	}
	m, err := snapshot.LoadSnapshot(o.db)
	if os.IsNotExist(err) {
		panic(util.UserError(fmt.Sprintf("No code map at %s: run `codemap build`.", o.db)))
	} else if err != nil {
		panic(util.UserError(err.Error()))
	}
	if v := m.MetaValue("index_version"); v != snapshot.IndexVersion {
		panic(util.UserError(fmt.Sprintf("The code map at %s is from another version of the tool: run `codemap build`.", o.db)))
	}
	var c config.Config
	if err := json.Unmarshal([]byte(m.MetaValue("config")), &c); err != nil {
		panic(util.UserError(fmt.Sprintf("The code map at %s has no settings: run `codemap build`.", o.db)))
	}
	config.ApplyConfig(&c)
	g := NewGraph(m, root)
	if !o.noCheck && cmd != "build" {
		g.checked, g.stale = true, g.findStale()
	}
	if g.stale != nil && o.strict { // the answer is printed only when no changed file is in it
		out := os.Stdout
		held, err := os.CreateTemp("", "codemap-*")
		if err != nil {
			panic(err)
		}
		defer os.Remove(held.Name())
		os.Stdout = held
		defer func() {
			os.Stdout = out
			if code != exitStale {
				held.Seek(0, 0)
				io.Copy(out, held)
			}
			held.Close()
		}()
	}
	code = g.run(cmd, o)
	return g.reportStale(o.strict, code)
}

func (g *Graph) run(cmd string, o opts) int {
	need := func(n int, what string) {
		if len(o.pos) < n {
			panic(util.UserError("the following arguments are required: " + what))
		}
	}
	switch cmd {
	case "used-by", "uses":
		need(1, "SYMBOL")
		return g.cmdEdges(cmd, o)
	case "impact":
		need(1, "SYMBOL")
		return g.cmdImpact(o)
	case "find":
		need(1, "PATTERN")
		return g.cmdFind(o)
	case "changes":
		return g.cmdChanges(o)
	case "nav-path":
		need(1, "SCREEN")
		return g.cmdNavPath(o)
	case "graph":
		need(1, "SCREEN")
		return g.cmdGraph(o)
	case "screens":
		return g.cmdScreens(o)
	case "build":
		return g.cmdBuildInfo(o)
	}
	return exitError
}

// ---------------------------------------------------------------- helpers

func (g *Graph) resolveOne(text string, allowMany bool) []Seed {
	found := g.resolve(text)
	if len(found) == 0 {
		parts := strings.Split(text, ".")
		panic(util.UserError(fmt.Sprintf("Not found: %s (try `codemap find %s`)", text, parts[len(parts)-1])))
	}
	tops := map[int32]bool{}
	for _, s := range found {
		tops[g.topOf(s.N)] = true
	}
	if len(found) > 1 && len(tops) > 1 && !allowMany {
		lines := []string{fmt.Sprintf("Ambiguous: %s matches %d symbols, give a qualified name or --all:", text, len(found))}
		for i, s := range found {
			if i == 30 {
				break
			}
			lines = append(lines, fmt.Sprintf("  %-60s %s", g.qualified(s.N), g.location(s.N)))
		}
		panic(util.UserError(strings.Join(lines, "\n")))
	}
	return found
}

func (g *Graph) qualified(n int32) string {
	if q := g.qualifieds[n]; q != "" {
		return q
	}
	q := g.makeQualified(n)
	if g.isMember(n) { // the others go through label, which records their file as shown
		g.qualifieds[n] = q
	}
	return q
}

func (g *Graph) makeQualified(n int32) string {
	switch g.kind(n) {
	case "C":
		return util.Dotted(g.name(n))
	case "M", "F":
		return util.Dotted(g.name(g.owner(n))) + "." + g.sname(n)
	}
	return g.label(n)
}

func (g *Graph) describe(n int32) string {
	return g.label(n) + "  " + g.module(n) + "  " + g.location(n)
}

func kindName(k string) string {
	if v, ok := kindNames[k]; ok {
		return v
	}
	return k
}

func (g *Graph) symbol(n int32) obj {
	return obj{{"symbol", g.qualified(n)}, {"kind", kindName(g.kind(n))}, {"module", g.module(n)}, {"location", g.location(n)}}
}

// pkg: the package of a class, member or field (ok false for other nodes).
func (g *Graph) pkg(n int32) (string, bool) {
	if !g.isMember(n) {
		return "", false
	}
	name := g.name(n)
	if t := g.m.Node.TopIx[n]; t >= 0 {
		name = g.name(t)
	}
	i := strings.LastIndex(name, "/")
	if i < 0 {
		return "", true
	}
	return strings.ReplaceAll(name[:i], "/", "."), true
}

func (g *Graph) printJSON(command string, result obj) {
	m := g.m
	var commit any
	if c := m.MetaValue("git_head"); c != "" {
		commit = c
	}
	duration := float64(time.Since(started).Milliseconds()/10) / 100
	var stale any
	if g.checked {
		stale = g.stale != nil
	}
	data := obj{
		{"schema_version", schemaVersion},
		{"command", command},
		{"meta", obj{
			{"tool_version", toolVersion},
			{"index", obj{{"version", m.MetaValue("index_version")}, {"commit", commit},
				{"dirty", m.MetaValue("dirty") == "1"}, {"inputs_sha1", m.MetaValue("inputs_sha1")},
				{"stale", stale}, {"stale_inputs", g.staleJSON()}}},
			{"refs", g.refs},
			{"run", obj{{"started_at", started.UTC().Format("2006-01-02T15:04:05Z")}, {"duration_s", duration}}},
		}},
	}
	data = append(data, result...)
	b := bufio.NewWriterSize(os.Stdout, 1<<20)
	writeJSON(b, data, 0)
	b.WriteString("\n")
	b.Flush()
}

// ---------------------------------------------------------------- used-by / uses

func (g *Graph) cmdEdges(cmd string, o opts) int {
	found := g.resolveOne(o.pos[0], o.all)
	var rows []EdgeRow
	for _, s := range found {
		if cmd == "used-by" {
			rows = append(rows, g.directUsers(s.N, s.Mode)...)
			continue
		}
		targets := []int32{s.N}
		if g.kind(s.N) == "C" && s.Mode == modeWhole {
			targets = append(targets, g.members(s.N)...)
		}
		for _, t := range targets {
			for _, u := range g.uses(t) {
				if u.Kind == snapshot.INJECT {
					continue
				}
				if g.topOf(u.N) == g.topOf(s.N) && (s.Mode == modeWhole || g.kind(u.N) == "C") {
					continue // the class's own members, lambdas and Compose `$stable`
				}
				if g.label(u.N) == strings.TrimSuffix(g.label(t), "()")+"{λ}" {
					continue // the method's own lambdas
				}
				rows = append(rows, EdgeRow{u.N, edgeKinds[u.Kind], t})
			}
		}
	}
	if o.noSelf && cmd == "used-by" {
		kept := rows[:0]
		for _, r := range rows {
			if g.topOf(r.Other) != g.topOf(r.Via) {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	outside := -1
	if mf := g.moduleFilter(o.modules); mf != nil {
		kept, hidden := rows[:0], map[int32]bool{}
		for _, r := range rows {
			if mf.keeps(g.module(r.Other)) {
				kept = append(kept, r)
			} else {
				hidden[r.Other] = true
			}
		}
		rows, outside = kept, len(hidden)
	}
	title := map[string]string{"used-by": "Used by", "uses": "Uses"}[cmd]
	return g.printEdges(found, rows, title, o.json, cmd, outside, o.noDI)
}

// printEdges: outside, the symbols --module left out (-1 without it); noDI leaves out generated DI code,
// still counted in generated_di.
func (g *Graph) printEdges(found []Seed, rows []EdgeRow, title string, asJSON bool, command string, outside int, noDI bool) int {
	isFound := map[int32]bool{}
	for _, s := range found {
		isFound[s.N] = true
	}
	generated := map[int32]bool{}
	kept := rows[:0]
	for _, r := range rows {
		if g.diGlue[g.topOf(r.Other)] {
			generated[r.Other] = true
			if noDI {
				continue
			}
		}
		kept = append(kept, r)
	}
	rows = kept
	groups := map[string]map[int32]map[string]bool{}
	var modOrder []string               // first-seen order: ties of the sort below keep it
	othersOrder := map[string][]int32{} // first-seen order
	for _, r := range rows {
		name := r.Kind
		if !isFound[r.Via] {
			l := g.label(r.Via)
			name += " " + l[strings.LastIndex(l, ".")+1:]
		}
		if generated[r.Other] {
			name += " (generated DI)"
		}
		mod := g.module(r.Other)
		if groups[mod] == nil {
			groups[mod] = map[int32]map[string]bool{}
			modOrder = append(modOrder, mod)
		}
		if groups[mod][r.Other] == nil {
			groups[mod][r.Other] = map[string]bool{}
			othersOrder[mod] = append(othersOrder[mod], r.Other)
		}
		groups[mod][r.Other][name] = true
	}
	if asJSON {
		var symbols []string
		for _, s := range found {
			symbols = append(symbols, g.qualified(s.N))
		}
		sort.Strings(symbols)
		type item struct {
			o                        obj
			module, symbol, location string
		}
		var items []item
		for _, mod := range modOrder {
			for _, other := range othersOrder[mod] {
				s := g.symbol(other)
				items = append(items, item{append(s, kv{"edges", util.SortedKeys(groups[mod][other])}), s.get("module"), s.get("symbol"), s.get("location")})
			}
		}
		sort.SliceStable(items, func(i, j int) bool {
			a, b := items[i], items[j]
			if a.module != b.module {
				return a.module < b.module
			}
			if a.symbol != b.symbol {
				return a.symbol < b.symbol
			}
			return a.location < b.location
		})
		out := make([]any, len(items))
		for i, it := range items {
			out[i] = it.o
		}
		result := obj{{"symbols", strs(symbols)}, {"items", out}, {"generated_di", len(generated)}}
		if outside >= 0 {
			result = append(result, kv{"hidden_by_module", obj{{"items", outside}}})
		}
		g.printJSON(command, result)
		return exitOK
	}
	for _, s := range found {
		fmt.Println(g.describe(s.N))
	}
	total := 0
	for _, v := range groups {
		total += len(v)
	}
	gen := ""
	if len(generated) > 0 && noDI {
		gen = fmt.Sprintf("; %d generated DI code not shown", len(generated))
	} else if len(generated) > 0 {
		gen = fmt.Sprintf(", %d of them generated DI code (--no-di leaves them out)", len(generated))
	}
	fmt.Printf("%s: %d in %d modules%s\n", title, total, len(groups), gen)
	for _, mod := range util.SortedKeys(groups) {
		fmt.Println("\n" + mod)
		others := othersOrder[mod]
		sort.SliceStable(others, func(i, j int) bool {
			li, lj := g.location(others[i]), g.location(others[j])
			if li != lj {
				return li < lj
			}
			return g.label(others[i]) < g.label(others[j])
		})
		for _, o := range others {
			fmt.Printf("  %-55s %-22s %s\n", g.label(o), strings.Join(util.SortedKeys(groups[mod][o]), ", "), g.location(o))
		}
	}
	if outside > 0 {
		fmt.Println("\n" + outsideLine(fmt.Sprintf("%d symbols", outside)))
	}
	return exitOK
}

// ---------------------------------------------------------------- impact

func shownKinds(o opts) map[string]bool {
	if len(o.kinds) == 0 {
		return screenKinds
	}
	out := map[string]bool{}
	for _, k := range o.kinds {
		if k == "all" {
			for _, t := range config.ScreenTitles {
				out[t[0]] = true
			}
		}
		out[k] = true
	}
	return out
}

func (g *Graph) cmdImpact(o opts) int {
	var seeds []Seed
	for _, s := range o.pos {
		seeds = append(seeds, g.resolveOne(s, o.all)...)
	}
	if o.to != "" {
		if len(o.modules) > 0 {
			panic(util.UserError("argument --module: not allowed with argument --to"))
		}
		return g.impactTo(seeds, o)
	}
	return g.reportImpact(seeds, o, "impact")
}

type screenItem struct {
	depth              int
	label, target, mod string
	hosts              []int32
	n                  int32
}

// openedFromNames: the screens opening a dialog, by name (listed for dialogs only).
func (g *Graph) openedFromNames(n int32) []string {
	names := []string{}
	if g.screens[n][0].Kind != "dialog" {
		return names
	}
	for _, s := range g.openedFrom(n) {
		names = append(names, g.screenName(s))
	}
	sort.Strings(names)
	return names
}

func (g *Graph) screenName(n int32) string {
	r := g.screens[n][0]
	if r.Kind == "preview" {
		return r.Target
	}
	return r.Label
}

func (g *Graph) reportImpact(seeds []Seed, o opts, command string) int {
	return g.reportImpactWith(seeds, o, command, nil, nil)
}

// reportImpactWith: nc, the changes left out; warnings, what may make the result wrong.
func (g *Graph) reportImpactWith(seeds []Seed, o opts, command string, nc []notCovered, warnings []string) int {
	mf := g.moduleFilter(o.modules) // the answer, not the walk: a screen elsewhere is reached through any module
	t := time.Now()
	w := newWalk()
	if len(seeds) > 0 {
		w = g.impact(seeds, o.depth)
	}
	elapsed := time.Since(t)
	byKind, hidden, shown := map[string][]screenItem{}, map[string]int{}, shownKinds(o)
	outScreens, outSymbols := 0, map[int32]bool{}
	for _, n := range w.ScreenOrder {
		r := g.screens[n][0]
		if !mf.keeps(g.module(n)) {
			outScreens++
		} else if shown[r.Kind] {
			byKind[r.Kind] = append(byKind[r.Kind], screenItem{w.Aff[w.Screens[n]].Depth, r.Label, r.Target, g.module(n), g.hosts(n), n})
		} else {
			hidden[r.Kind]++
		}
	}
	for _, items := range byKind {
		sort.SliceStable(items, func(i, j int) bool {
			a, b := items[i], items[j]
			if a.depth != b.depth {
				return a.depth < b.depth
			}
			if a.label != b.label {
				return a.label < b.label
			}
			if a.target != b.target {
				return a.target < b.target
			}
			return a.mod < b.mod
		})
	}
	navs := map[int32]*navOf{}
	if o.nav {
		for _, items := range byKind {
			for _, it := range items {
				if screenKinds[g.screens[it.n][0].Kind] {
					navs[it.n] = g.navOfScreen(it.n)
				}
			}
		}
	}
	elapsedNav := time.Since(t) - elapsed
	seenNC := map[notCovered]bool{}
	var notCov []notCovered
	for _, x := range nc {
		if !seenNC[x] {
			seenNC[x] = true
			notCov = append(notCov, x)
		}
	}
	sort.Slice(notCov, func(i, j int) bool {
		if notCov[i].path != notCov[j].path {
			return notCov[i].path < notCov[j].path
		}
		return notCov[i].reason < notCov[j].reason
	})
	seedIDs := map[int32]bool{}
	for _, s := range seeds {
		seedIDs[s.N] = true
	}
	modules, packages := map[string]int{}, map[string]int{}
	nearest := map[int32]Key{}
	var nearestOrder []int32
	for _, k := range w.Keys {
		if !mf.keeps(g.module(k.N)) {
			if !seedIDs[k.N] {
				outSymbols[k.N] = true
			}
			continue
		}
		modules[g.module(k.N)]++
		if p, ok := g.pkg(k.N); ok {
			packages[p]++
		}
		old, ok := nearest[k.N]
		if !ok {
			nearestOrder = append(nearestOrder, k.N)
		}
		if !ok || g.before(w, k, old) {
			nearest[k.N] = k
		}
	}
	if o.json {
		listed := map[int32]bool{}
		var changed []obj
		for _, s := range seeds {
			if !listed[s.N] {
				listed[s.N] = true
				changed = append(changed, g.symbol(s.N))
			}
		}
		sortObjs(changed, "symbol", "location", "kind")
		var affected []obj
		for _, n := range nearestOrder {
			if seedIDs[n] {
				continue
			}
			k := nearest[n]
			affected = append(affected, append(g.symbol(n), kv{"depth", w.Aff[k].Depth}, kv{"path", g.qualifiedPath(g.path(w, k))}))
		}
		sort.SliceStable(affected, func(i, j int) bool {
			a, b := affected[i], affected[j]
			if da, db := a.getInt("depth"), b.getInt("depth"); da != db {
				return da < db
			}
			if a.get("symbol") != b.get("symbol") {
				return a.get("symbol") < b.get("symbol")
			}
			return a.get("location") < b.get("location")
		})
		var screens []any
		for _, kind := range util.SortedKeys(byKind) {
			for _, it := range byKind[kind] {
				hostNames := []string{}
				for _, h := range it.hosts {
					hostNames = append(hostNames, g.label(h))
				}
				var host any
				if len(hostNames) > 0 {
					host = hostNames[0]
				}
				s := obj{{"kind", kind}, {"name", it.label}, {"target", it.target}, {"module", it.mod},
					{"location", g.location(it.n)}, {"host", host}, {"hosts", strs(hostNames)}, {"depth", it.depth},
					{"opened_from", strs(g.openedFromNames(it.n))},
					{"path", g.qualifiedPath(g.path(w, w.Screens[it.n]))}}
				if nav, ok := navs[it.n]; ok {
					s = append(s, kv{"nav", nav.json()})
				}
				screens = append(screens, s)
			}
		}
		var ncJSON []any
		for _, x := range notCov {
			ncJSON = append(ncJSON, obj{{"path", x.path}, {"reason", x.reason}})
		}
		result := obj{
			{"changed", objs(changed)},
			{"affected", objs(affected)},
			{"screens", list(screens)},
			{"hidden", countsObj(hidden)},
			{"modules", countsObj(modules)},
			{"packages", countsObj(packages)},
			{"not_covered", list(ncJSON)},
			{"warnings", strs(warnings)},
		}
		if mf != nil {
			result = append(result, kv{"hidden_by_module", obj{{"screens", outScreens}, {"affected", len(outSymbols)}}})
		}
		g.printJSON(command, result)
		return exitOK
	}
	if len(seeds) > 0 {
		fmt.Println("Changed:")
	} else {
		fmt.Println("Changed: nothing the code map analyses")
	}
	for _, s := range seeds {
		whole := ""
		if s.Mode == modeWhole && g.kind(s.N) == "C" {
			whole = "  (whole class)"
		}
		fmt.Printf("  %s%s\n", g.describe(s.N), whole)
	}
	for _, w := range warnings {
		fmt.Println("  warning: " + w)
	}
	total := 0
	var counts []string
	for _, t := range config.ScreenTitles {
		total += len(byKind[t[0]])
		if len(byKind[t[0]]) > 0 {
			counts = append(counts, fmt.Sprintf("%s %d", t[0], len(byKind[t[0]])))
		}
	}
	fmt.Printf("\nScreens: %d  (%s)\n", total, strings.Join(counts, ", "))
	for _, t := range config.ScreenTitles {
		items := byKind[t[0]]
		if len(items) == 0 {
			continue
		}
		fmt.Println("\n" + t[1])
		for _, it := range items {
			extra := ""
			if len(it.hosts) > 0 {
				var hl []string
				for _, h := range it.hosts {
					hl = append(hl, g.label(h))
				}
				extra = "  host " + strings.Join(hl, ", ")
			}
			name := it.label
			if t[0] == "preview" {
				name = it.target
			}
			fmt.Println(strings.TrimRight(fmt.Sprintf("  %-50s %-28s%s", name, it.mod, extra), " "))
			if g.screens[it.n][0].Kind == "dialog" {
				names := g.openedFromNames(it.n)
				if len(names) == 0 {
					names = []string{"not found"}
				}
				fmt.Println("      opened from: " + strings.Join(names, ", "))
			}
			if nav, ok := navs[it.n]; ok {
				if nav == nil {
					fmt.Println("      nav: not found")
				} else if nav.entry == "launcher" {
					fmt.Println("      nav: " + nav.text)
				} else {
					fmt.Printf("      nav (%s): %s\n", nav.entry, nav.text)
				}
			}
			if !o.noPaths {
				fmt.Println("      " + g.formatPath(g.path(w, w.Screens[it.n])))
			}
		}
	}
	if len(hidden) > 0 {
		fmt.Println("\n" + hiddenLine(hidden))
	}
	if outScreens+len(outSymbols) > 0 {
		fmt.Println("\n" + outsideLine(fmt.Sprintf("%d screens", outScreens), fmt.Sprintf("%d symbols", len(outSymbols))))
	}
	if len(modules) > 0 {
		mods := util.SortedKeys(modules)
		sort.SliceStable(mods, func(i, j int) bool { return modules[mods[i]] > modules[mods[j]] })
		var parts []string
		for _, m := range mods {
			parts = append(parts, fmt.Sprintf("%s %d", m, modules[m]))
		}
		fmt.Println("\nModules (affected symbols): " + strings.Join(parts, ", "))
	}
	if len(notCov) > 0 {
		fmt.Println("\nNot covered:")
		for _, t := range notCoveredTitles {
			var paths []string
			for _, x := range notCov {
				if x.reason == t[0] {
					paths = append(paths, x.path)
				}
			}
			if len(paths) == 0 {
				continue
			}
			more := ""
			if len(paths) > 15 {
				paths, more = paths[:15], fmt.Sprintf(" ... %d more", len(paths)-15)
			}
			fmt.Printf("  %s: %s%s\n", t[1], strings.Join(paths, ", "), more)
		}
	}
	nav := ""
	if o.nav {
		nav = fmt.Sprintf(", nav %.2fs", elapsedNav.Seconds())
	}
	fmt.Printf("(%d symbols affected, %.2fs%s)\n", len(w.Keys), elapsed.Seconds(), nav)
	return exitOK
}

// ---------------------------------------------------------------- screens / build --info

func (g *Graph) cmdScreens(o opts) int {
	filter := ""
	if len(o.pos) > 0 {
		filter = strings.ToLower(o.pos[0])
	}
	type row struct {
		kind, module, label, target, host, loc string
		hosts                                  []int32
		ends                                   []navRoot
	}
	var out []row
	hidden, shown := map[string]int{}, shownKinds(o)
	mf, outside := g.moduleFilter(o.modules), 0
	var hostIn map[int32]bool
	if o.host != "" {
		hostIn = map[int32]bool{}
		for _, h := range g.screensOf(o.host, o.all) {
			hostIn[h] = true
		}
	}
	s := &g.m.Screen
	for i := range s.Node {
		n := g.m.Index(s.Node[i])
		kind, label, target := s.Kind.At(i), s.Label.At(i), s.Target.At(i)
		module, hosts := g.module(n), g.hosts(n)
		var hl []string
		for _, h := range hosts {
			hl = append(hl, g.label(h))
		}
		host := strings.Join(hl, ", ")
		if filter != "" && !slices.ContainsFunc([]string{label, target, module, host},
			func(x string) bool { return strings.Contains(strings.ToLower(x), filter) }) {
			continue
		}
		if !mf.keeps(module) {
			outside++
			continue
		}
		if hostIn != nil && !slices.ContainsFunc(hosts, func(h int32) bool { return hostIn[h] }) {
			continue
		}
		if !shown[kind] {
			hidden[kind]++
			continue
		}
		var ends []navRoot
		if o.unreachable {
			if !screenKinds[kind] {
				continue // previews and layouts are not opened, so not reached either
			}
			var dead bool
			if ends, dead = g.unreached(n); !dead {
				continue
			}
		}
		out = append(out, row{kind, module, label, target, host, g.location(n), hosts, ends})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		for _, p := range [][2]string{{a.kind, b.kind}, {a.module, b.module}, {a.label, b.label}, {a.target, b.target},
			{a.host, b.host}, {a.loc, b.loc}} {
			if p[0] != p[1] {
				return p[0] < p[1]
			}
		}
		return slices.Compare(a.hosts, b.hosts) < 0
	})
	if o.json {
		items := make([]any, len(out))
		for i, r := range out {
			item := append(append(obj{{"kind", r.kind}, {"module", r.module}, {"name", r.label}, {"target", r.target}},
				g.hostsJSON(r.hosts)...), kv{"location", r.loc})
			if o.unreachable {
				var ends []any
				for _, e := range r.ends {
					ends = append(ends, g.entryJSON(e.sid, e.how))
				}
				item = append(item, kv{"entries", list(ends)})
			}
			items[i] = item
		}
		result := obj{{"screens", items}, {"hidden", countsObj(hidden)}}
		if mf != nil {
			result = append(result, kv{"hidden_by_module", obj{{"screens", outside}}})
		}
		g.printJSON("screens", result)
		return exitOK
	}
	for _, t := range config.ScreenTitles {
		var items []row
		for _, r := range out {
			if r.kind == t[0] {
				items = append(items, r)
			}
		}
		if len(items) == 0 {
			continue
		}
		fmt.Printf("\n%s (%d)\n", t[1], len(items))
		for _, r := range items {
			name := r.label
			if r.kind == "preview" {
				name = r.target
			}
			host := ""
			if r.host != "" {
				host = "  host " + r.host
			}
			fmt.Printf("  %-50s %-28s %s%s\n", name, r.module, r.loc, host)
			for _, e := range r.ends {
				fmt.Printf("      ends at %s: %s\n", g.screenName(e.sid), deadEnd(e.how[0]))
			}
		}
	}
	if len(hidden) > 0 {
		fmt.Println("\n" + hiddenLine(hidden))
	}
	if outside > 0 {
		fmt.Println("\n" + outsideLine(fmt.Sprintf("%d screens", outside)))
	}
	if o.unreachable {
		fmt.Printf("\n(%d screens no entry of the manifest reaches, as far as the code map sees)\n", len(out))
	}
	return exitOK
}

// deadEnd: how a way back from an unreachable screen ends, for reading.
func deadEnd(e navEntry) string {
	switch e.kind {
	case "unknown":
		return "nothing opens it"
	case "outside":
		return "opened only from code outside the screens"
	}
	return entryText(e)
}

func (g *Graph) cmdBuildInfo(o opts) int {
	screens := map[string]int{}
	for i := range g.m.Screen.Node {
		screens[g.m.Screen.Kind.At(i)]++
	}
	meta := map[string]string{}
	for i := range g.m.Meta.Key.Len() {
		meta[g.m.Meta.Key.At(i)] = g.m.Meta.Value.At(i)
	}
	if o.json {
		index := obj{}
		for _, k := range util.SortedKeys(meta) {
			index = append(index, kv{k, meta[k]})
		}
		g.printJSON("build", obj{{"index", index}, {"screens", countsObj(screens)}})
		return exitOK
	}
	for _, k := range util.SortedKeys(meta) {
		v := meta[k]
		if k == "class_dirs" {
			var dirs []string
			json.Unmarshal([]byte(v), &dirs)
			v = fmt.Sprintf("%d directories", len(dirs))
		}
		if k == "config" || k == "module_paths" {
			continue // long JSON: `codemap config print`, codemap.model.json
		}
		fmt.Printf("%-14s %s\n", k, v)
	}
	for _, k := range util.SortedKeys(screens) {
		fmt.Printf("screens.%-6s %d\n", k, screens[k])
	}
	return exitOK
}

func hiddenLine(hidden map[string]int) string {
	var parts []string
	for _, t := range config.ScreenTitles {
		if hidden[t[0]] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", t[0], hidden[t[0]]))
		}
	}
	return "Not shown: " + strings.Join(parts, ", ") + " (--kind KIND, --kind all)"
}

func (g *Graph) qualifiedPath(path []Step) []any {
	out := make([]any, len(path))
	for i, s := range path {
		out[i] = g.qualified(s.N)
	}
	return out
}

// sourceLabel: a node as the source names it: a lambda as the method holding it.
func (g *Graph) sourceLabel(n int32) string {
	l := g.label(n)
	if strings.HasSuffix(l, "{λ}") {
		l = strings.TrimSuffix(l, "{λ}")
		if strings.Contains(l, ".") {
			l += "()"
		}
	}
	return l
}

// formatPath: a walk path for reading.
func (g *Graph) formatPath(path []Step) string {
	type st struct{ text, full string }
	var steps []st
	owner := ""
	hasOwner := false
	for _, s := range path {
		if g.kind(s.N) == "ROUTE" && len(steps) > 0 {
			break
		}
		l := g.sourceLabel(s.N)
		if len(steps) > 0 && steps[len(steps)-1].full == l {
			continue
		}
		if len(steps) > 0 && steps[len(steps)-1].full == l+".<init>()" {
			steps = steps[:len(steps)-1]
		}
		cls, member := l, ""
		if strings.HasSuffix(l, ")") && strings.Contains(l, ".") {
			b := l[:len(l)-2]
			i := strings.LastIndex(b, ".")
			if i >= 0 {
				cls, member = b[:i], b[i+1:]
			} else {
				cls = b
			}
		}
		text := l
		if member != "" && hasOwner && cls == owner {
			text = "." + member + "()"
		}
		steps = append(steps, st{text, l})
		owner, hasOwner = cls, true
	}
	parts := make([]string, len(steps))
	for i, s := range steps {
		parts[i] = s.text
	}
	return strings.Join(parts, " -> ")
}

func whyOf(path []Step, n int32) string {
	for i, s := range path {
		if s.N == n && i+1 < len(path) {
			return path[i+1].Why
		}
	}
	return ""
}

// impactTo: the shortest path from the change to a screen named TARGET, else to the symbol TARGET or
// code of its top-level class.
func (g *Graph) impactTo(seeds []Seed, o opts) int {
	w := g.impact(seeds, o.depth)
	var named []int32
	for _, n := range g.screenOrder {
		r := g.screens[n][0]
		if o.to == r.Label || o.to == r.Target {
			named = append(named, n)
		}
	}
	var hit *Key
	if len(named) > 0 {
		for _, n := range named {
			if k, ok := w.Screens[n]; ok && (hit == nil || g.before(w, k, *hit)) {
				hit = &k
			}
		}
	} else {
		sources, tops := map[int32]bool{}, map[int32]bool{}
		for _, s := range g.resolveOne(o.to, o.all) {
			sources[s.N] = true
			tops[g.topOf(s.N)] = true
		}
		for _, k := range w.Keys {
			if sources[k.N] || g.isMember(k.N) && tops[g.topOf(k.N)] {
				if hit == nil || g.before(w, k, *hit) {
					k := k
					hit = &k
				}
			}
		}
	}
	var path []Step
	if hit != nil {
		path = g.path(w, *hit)
	}
	changed := strings.Join(o.pos, ", ")
	if o.json {
		set := map[string]bool{}
		for _, s := range seeds {
			set[g.qualified(s.N)] = true
		}
		var steps []any
		for i := len(path) - 1; i >= 0; i-- {
			n := path[i].N
			var edge any
			if e := whyOf(path, n); e != "" {
				edge = e
			}
			steps = append(steps, append(g.symbol(n), kv{"edge", edge}))
		}
		g.printJSON("impact", obj{{"to", o.to}, {"changed", strs(util.SortedKeys(set))}, {"steps", list(steps)}})
		return exitOK
	}
	if hit == nil {
		fmt.Printf("No dependency path: %s does not depend on %s (as far as the code map sees).\n", o.to, changed)
		return exitOK
	}
	fmt.Printf("%s depends on %s in %d steps:\n", o.to, changed, len(path)-1)
	for i := len(path) - 1; i >= 0; i-- {
		n := path[i].N
		prefix := ""
		if i != len(path)-1 {
			prefix = "<-[" + whyOf(path, n) + "]- "
		}
		fmt.Printf("  %s%-50s %s\n", prefix, g.label(n), g.location(n))
	}
	return exitOK
}

// ---------------------------------------------------------------- find

// likeMatch: SQLite LIKE (ASCII case-insensitive, % any run, _ any one byte) for a pattern that
// starts and ends with %: its segments in order, each at its leftmost place.
func likeMatch(segs []string, s string) bool {
	pos := 0
	for _, seg := range segs {
		at := -1
		for i := pos; i+len(seg) <= len(s); i++ {
			if foldHas(s[i:], seg) {
				at = i
				break
			}
		}
		if at < 0 {
			return false
		}
		pos = at + len(seg)
	}
	return true
}

func foldHas(s, seg string) bool {
	for j := 0; j < len(seg); j++ {
		c, p := s[j], seg[j]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if p != '_' && c != p {
			return false
		}
	}
	return true
}

func (g *Graph) cmdFind(o opts) int {
	pattern := o.pos[0]
	rx, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		rx = regexp.MustCompile("(?i)" + regexp.QuoteMeta(pattern))
	}
	var like []string
	for _, seg := range strings.Split(strings.ToLower(regexp.MustCompile(`[^\w]`).ReplaceAllString(pattern, "%")), "%") {
		if seg != "" {
			like = append(like, seg)
		}
	}
	type row struct{ kind, q, module, loc string }
	mf, outside := g.moduleFilter(o.modules), 0
	seen := map[row]bool{}
	var out []row
	synthetic := map[string]bool{}
	for i := range g.m.Nodes() {
		n := int32(i)
		k := g.kind(n)
		if k == "ROUTE" || !(likeMatch(like, g.sname(n)) || likeMatch(like, g.name(n))) {
			continue
		}
		name := g.name(n)
		if k == "M" && (strings.HasPrefix(name, "access$") || strings.HasPrefix(name, "<") || strings.Contains(name, "$lambda")) {
			continue
		}
		q := g.qualified(n)
		if !(rx.MatchString(q) || rx.MatchString(g.sname(n))) {
			continue
		}
		if len(o.types) > 0 && !slices.Contains(o.types, kindName(k)) {
			continue
		}
		cls := n
		if k != "C" {
			cls = g.owner(n)
		}
		if !o.all && cls >= 0 && (isDIGlue(g.name(cls)) || syntheticClass.MatchString(g.name(cls))) {
			synthetic[q] = true
			continue
		}
		r := row{k, q, g.module(n), g.location(n)}
		if seen[r] {
			continue
		}
		seen[r] = true
		if mf.keeps(r.module) {
			out = append(out, r)
		} else {
			outside++
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if la, lb := utf8.RuneCountInString(a.q), utf8.RuneCountInString(b.q); la != lb {
			return la < lb
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		if a.q != b.q {
			return a.q < b.q
		}
		if a.module != b.module {
			return a.module < b.module
		}
		return a.loc < b.loc
	})
	shownRows := out
	if o.limit >= 0 && o.limit < len(out) {
		shownRows = out[:o.limit]
	}
	if o.json {
		syms := make([]any, len(shownRows))
		for i, r := range shownRows {
			syms[i] = obj{{"symbol", r.q}, {"kind", kindName(r.kind)}, {"module", r.module}, {"location", r.loc}}
		}
		result := obj{{"pattern", pattern}, {"total", len(out)}, {"synthetic", len(synthetic)}, {"symbols", syms}}
		if mf != nil {
			result = append(result, kv{"hidden_by_module", obj{{"symbols", outside}}})
		}
		g.printJSON("find", result)
		return exitOK
	}
	for _, r := range shownRows {
		fmt.Printf("%-5s %-70s %-28s %s\n", r.kind, r.q, r.module, r.loc)
	}
	if len(shownRows) < len(out) {
		fmt.Printf("... %d more (--limit)\n", len(out)-len(shownRows))
	}
	if len(synthetic) > 0 {
		fmt.Printf("(%d synthetic hidden: Hilt glue, lambdas, DefaultImpls; --all shows them)\n", len(synthetic))
	}
	if outside > 0 {
		fmt.Printf("(%s)\n", outsideLine(fmt.Sprintf("%d symbols", outside)))
	}
	return exitOK
}
