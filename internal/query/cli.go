package query

// The query commands: options, dispatch and the helpers they share; each command is in a file of its own.

import (
	"bufio"
	"cmp"
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
	schemaVersion = "3.6.0"
	exitOK        = 0
	exitError     = 2
)

var (
	kindNames      = map[string]string{"C": "class", "M": "method", "F": "field", "R": "resource", "ROUTE": "route"}
	findTypes      = []string{"class", "method", "field", "resource"} // find --type: find lists no routes
	syntheticClass = regexp.MustCompile(`\$(\d|\$|DefaultImpls$|WhenMappings$)|_HiltModules_\w+$`)
	started        = time.Now()
	ToolVersion    = "dev" // set by the command from the release build or the module version
)

type opts struct {
	db, variant, to, base, head, diff, files, from, entry, direction, host string
	json, all, noPaths, noSelf, noDI, noCheck, nav                         bool
	strict, verbose, info, ways, in, unreachable                           bool
	depth, limit                                                           int
	kinds, pos, via, avoid, modules, types                                 []string
}

// fields: where each option goes: a switch sets its bool, a value its string, a repeatable one
// appends, a number its int.
func (o *opts) fields() (map[string]*bool, map[string]*string, map[string]*[]string, map[string]*int) {
	switches := map[string]*bool{"verbose": &o.verbose, "info": &o.info, "json": &o.json, "all": &o.all,
		"no-paths": &o.noPaths, "no-self": &o.noSelf, "no-di": &o.noDI, "unreachable": &o.unreachable,
		"no-check": &o.noCheck, "strict": &o.strict, "nav": &o.nav, "ways": &o.ways, "in": &o.in}
	values := map[string]*string{"db": &o.db, "variant": &o.variant, "to": &o.to, "base": &o.base, "head": &o.head, "diff": &o.diff,
		"files": &o.files, "host": &o.host, "from": &o.from, "entry": &o.entry, "direction": &o.direction}
	lists := map[string]*[]string{"kind": &o.kinds, "type": &o.types, "via": &o.via, "avoid": &o.avoid,
		"module": &o.modules}
	numbers := map[string]*int{"depth": &o.depth, "limit": &o.limit}
	return switches, values, lists, numbers
}

func parseOpts(cmd string, args []string) opts {
	o := opts{depth: -1, limit: -1}
	spec := commandSpec(cmd)
	switches, values, lists, numbers := o.fields()
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
	for i := 0; i < len(args); i++ {
		a := args[i]
		hasInline = false
		if name, v, ok := strings.Cut(a, "="); ok && strings.HasPrefix(a, "--") {
			a, inline, hasInline = name, v, true
		}
		if !strings.HasPrefix(a, "--") {
			o.pos = append(o.pos, a)
			continue
		}
		name := a[2:]
		if !spec.hasFlag(name) {
			if knownFlag(name) {
				panic(util.UserError(fmt.Sprintf("argument %s: not an option of %s (codemap %s --help)", a, spec.name, cmd)))
			}
			panic(util.UserError("unrecognized arguments: " + a))
		}
		if p, ok := switches[name]; ok {
			*p = true
		} else if p, ok := values[name]; ok {
			*p = value(&i, name)
		} else if p, ok := lists[name]; ok {
			*p = append(*p, value(&i, name))
		} else if p, ok := numbers[name]; ok {
			*p = nonNegative(value(&i, name), name)
		} else {
			panic(util.UserError("unrecognized arguments: " + a))
		}
		if hasInline {
			panic(util.UserError(fmt.Sprintf("argument %s: takes no value, got '%s'", a, inline)))
		}
	}
	o.direction = strings.ToUpper(o.direction)
	if o.db != "" && o.variant != "" {
		panic(util.UserError("argument --variant: not with --db (the map at --db has its own variant)"))
	}
	o.check(spec)
	return o
}

func nonNegative(s, name string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		panic(util.UserError("argument --" + name + ": invalid int value"))
	}
	if v < 0 {
		panic(util.UserError("argument --" + name + ": expected N >= 0"))
	}
	return v
}

// check: the positionals the command takes and the values of the options with a set of choices.
func (o *opts) check(spec *command) {
	if n := spec.maxArgs(); n >= 0 && len(o.pos) > n {
		panic(util.UserError("unrecognized arguments: " + strings.Join(o.pos[n:], " ")))
	}
	choice := func(flag, v string, choices []string) {
		if !slices.Contains(choices, v) {
			panic(util.UserError(fmt.Sprintf("argument --%s: invalid choice: '%s' (choose from %s)", flag, v,
				strings.Join(choices, ", "))))
		}
	}
	if o.direction != "" {
		choice("direction", o.direction, mermaidDirections)
	}
	if o.entry != "" {
		choice("entry", o.entry, entryKinds)
	}
	for _, t := range o.types {
		choice("type", t, findTypes)
	}
	for _, k := range o.kinds {
		if k != "all" && !slices.ContainsFunc(config.ScreenTitles, func(t [2]string) bool { return t[0] == k }) {
			panic(util.UserError(fmt.Sprintf("argument --kind: invalid choice: '%s'", k)))
		}
	}
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
	rebuild := "codemap build"
	if o.variant != "" {
		rebuild += " --variant " + o.variant
	}
	if o.db == "" {
		var err error
		if o.db, err = config.SnapshotPath(root, o.variant); err != nil {
			panic(util.UserError(err.Error()))
		}
	}
	m, err := snapshot.LoadSnapshot(o.db)
	if os.IsNotExist(err) {
		panic(util.UserError(fmt.Sprintf("No code map at %s: run `%s`.", o.db, rebuild)))
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
			{"tool_version", ToolVersion},
			{"index", obj{{"version", m.MetaValue("index_version")}, {"variant", cmp.Or(config.Cfg.Variant, "debug")}, {"commit", commit},
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
