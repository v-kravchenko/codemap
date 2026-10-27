package query

// The query commands: options, dispatch and the helpers they share; each command is in a file of its own.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/snapshot"
	"github.com/v-kravchenko/codemap/internal/util"
)

const (
	toolVersion   = "0.2.0"
	schemaVersion = "3.5.0"
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
	defer util.Profile()()
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

func (g *Graph) screenName(n int32) string {
	r := g.screens[n][0]
	if r.Kind == "preview" {
		return r.Target
	}
	return r.Label
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
