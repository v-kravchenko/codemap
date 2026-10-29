package query

// The commands and their options: one table drives the usage, `codemap CMD --help` and the check
// that every option given means something to the command, so a misplaced flag is an error, not a
// silent no-op.

import (
	"fmt"
	"strings"
)

// flagSpec: an option; arg names its value ("" for a switch), many marks a repeatable one.
type flagSpec struct {
	name, arg, help string
	many            bool
}

// command: args names the positionals ("" none, "X" one, "[X]" at most one, "X..." one or more).
type command struct {
	name, args, about string
	flags             []flagSpec
	query             bool // reads the code map: takes the common options
}

var (
	optAll      = flagSpec{name: "all", help: "every match of an ambiguous name instead of an error"}
	optDepth    = flagSpec{name: "depth", arg: "N", help: "stop the walk after N steps (default: no limit)"}
	optKind     = flagSpec{name: "kind", arg: "KIND", many: true, help: "list screens of this kind: activity, fragment, dialog, route, preview, layout or all (default: the first four)"}
	optDirect   = flagSpec{name: "direct", help: "only the screens the change reaches through their own code, not through a navigator, a hub or a base class"}
	optNoPrev   = flagSpec{name: "no-preview", help: "only the screens no preview draws (activities, fragments, dialogs, routes)"}
	optNoPaths  = flagSpec{name: "no-paths", help: "leave out the way to each screen (text only; --json always has it)"}
	optNav      = flagSpec{name: "nav", help: "add how the user reaches each affected screen"}
	optAvoid    = flagSpec{name: "avoid", arg: "GLOB", many: true, help: "no way through a screen whose name or class matches (*: any run of characters)"}
	optModule   = flagSpec{name: "module", arg: "GLOB", many: true, help: "show only what is in a matching module: the module and those under it (*: one level, **: any, !GLOB: not)"}
	optJSON     = flagSpec{name: "json", help: "the answer as JSON (docs/json.md)"}
	optDB       = flagSpec{name: "db", arg: "FILE", help: "the code map (default: <variant>.snap in snapshot_dir of codemap.yml)"}
	optVariant  = flagSpec{name: "variant", arg: "VARIANT", help: "the code map of this build variant (default: variant in codemap.yml; not with --db)"}
	commonFlags = []flagSpec{
		optJSON, optDB, optVariant,
		{name: "no-check", help: "skip the check that the map is newer than its inputs"},
		{name: "strict", help: "exit 3 when the map misses modules not compiled, is stale and the answer uses a changed file, or (changes) the --diff does not match its sources"},
	}
)

var commands = []command{
	{name: "build", about: "Build the code map from the compiled classes and resources (skipped when nothing changed).",
		flags: []flagSpec{
			{name: "root", arg: "DIR", help: "project root (default: found from the working directory)"},
			{name: "out", arg: "SNAPSHOT", help: "where to write the map (default: <variant>.snap in snapshot_dir of codemap.yml)"},
			{name: "variant", arg: "VARIANT", help: "the build variant whose classes are read (default: variant in codemap.yml)"},
			{name: "jobs", arg: "N", help: "parallel workers (default: CPUs - 1)"},
			{name: "force", help: "rebuild even when the inputs did not change"},
		}},
	{name: "build --info", about: "What the code map holds and what it was built from.",
		flags: []flagSpec{optJSON, optDB, optVariant}},
	{name: "used-by", args: "SYMBOL", query: true, about: "What uses a class, method or field directly.",
		flags: []flagSpec{optAll, {name: "no-self", help: "leave out uses from the symbol's own top-level class"},
			{name: "no-di", help: "leave out generated DI code (Hilt/Dagger factories, components, modules)"}, optModule}},
	{name: "uses", args: "SYMBOL", query: true, about: "What a class, method or field uses directly.",
		flags: []flagSpec{optAll, optModule}},
	{name: "impact", args: "SYMBOL...", query: true, about: "What a change to the symbols reaches, up to the screens.",
		flags: []flagSpec{optDepth, optKind, optNoPrev, optDirect, optNoPaths,
			{name: "to", arg: "TARGET", help: "only the shortest way to this screen or symbol"},
			optNav, optModule, optAll}},
	{name: "changes", query: true, about: "The impact of a change: git against the base branch, a diff or a list of files.",
		flags: []flagSpec{
			{name: "base", arg: "REF", help: "the base to compare with (default: the first of changes.base in codemap.yml that resolves)"},
			{name: "head", arg: "REF", help: "the commit to compare instead of the working tree; the map must be built from it"},
			{name: "base-db", arg: "FILE", help: "the code map of the base: added and removed by the two maps, not by the base's text"},
			{name: "diff", arg: "-|FILE", help: "a unified diff from stdin or a file instead of git diff"},
			{name: "files", arg: "-|FILE", help: "lines of path, path:N or path:N-M instead of a diff"},
			optNav, optDepth, optKind, optNoPrev, optDirect, optNoPaths, optModule}},
	{name: "crash", args: "[-|FILE]", query: true, about: "Where a crash's stack trace ran, and the screens and components it ran for.",
		flags: []flagSpec{
			{name: "mapping", arg: "FILE", help: "the R8/ProGuard mapping.txt of the build, for an obfuscated trace"},
			optNav, optDepth, optKind, optNoPrev, optDirect, optNoPaths, optModule}},
	{name: "diff", query: true, about: "What this map has and another has not: screens, manifest entries, the screens that open them.",
		flags: []flagSpec{
			{name: "base", arg: "FILE", help: "the other code map: another variant's (build/codemap/release.snap) or another revision's"},
			optModule}},
	{name: "nav-path", args: "SCREEN", query: true, about: "How a user gets to a screen: the cheapest way from an entry, and the branches.",
		flags: []flagSpec{
			{name: "via", arg: "SCREEN", many: true, help: "the cheapest way through these screens, in order"},
			{name: "ways", help: "the cheapest way through each screen that opens the fork (not with --via)"},
			{name: "from", arg: "SCREEN", help: "ways from this screen instead of from an entry"},
			{name: "entry", arg: "KIND", help: "ways only from entries of this kind: launcher, deeplink, intent or push (not with --from)"},
			optAvoid,
			{name: "verbose", help: "every way found and the code of each step, not only the cheapest"},
			{name: "limit", arg: "N", help: "how many branches to show (default 20)"},
			{name: "depth", arg: "N", help: "stop the walk N screens back (default: no limit)"},
			optNoPaths, optAll}},
	{name: "graph", args: "SCREEN", query: true, about: "What a screen opens, as a Mermaid flowchart.",
		flags: []flagSpec{
			{name: "to", arg: "SCREEN", help: "only the ways that reach this screen (not with --in)"},
			{name: "in", help: "the other way: what opens the screen, and what opens those"},
			{name: "avoid", arg: "GLOB", many: true, help: "leave out screens whose name or class matches, and what only they reach"},
			{name: "module", arg: "GLOB", many: true, help: "walk on only from screens in a matching module (globs as in impact --module)"},
			{name: "depth", arg: "N", help: "how many steps to draw (default 1; with --to the fewest steps plus 2)"},
			{name: "direction", arg: "LR|RL|TB|TD|BT", help: "the way the flowchart runs (default LR)"},
			optAll}},
	{name: "screens", args: "[FILTER]", query: true, about: "The screens by kind; FILTER keeps those whose name, class, module, host or drawn composable contains it.",
		flags: []flagSpec{optKind, optNoPrev, optModule,
			{name: "host", arg: "SCREEN", help: "only screens hosted in this screen (an Activity, or the screen hosting routes)"},
			{name: "unreachable", help: "only screens no entry of the manifest reaches, with where the way back ends"}}},
	{name: "find", args: "PATTERN", query: true, about: "Classes, members, resources and routes whose name matches a regexp (any case).",
		flags: []flagSpec{
			{name: "all", help: "also generated DI and synthetic classes"},
			{name: "limit", arg: "N", help: "how many matches to show"},
			{name: "type", arg: "TYPE", many: true, help: "only symbols of this type: class, method, field or resource"},
			optModule}},
	{name: "version", about: "Print the version of codemap."},
	{name: "init", about: "Run Gradle once (configuration only) and write the project model, codemap.model.json.",
		flags: []flagSpec{
			{name: "root", arg: "DIR", help: "project root (default: found from the working directory)"},
			{name: "gradle", arg: "CMD", help: "the Gradle command (default: $CODEMAP_GRADLE, else ./gradlew)"},
		}},
	{name: "wrapper", about: "Write codemapw (and .cmd), codemap-wrapper.properties and gradle/codemap.gradle: a checkout runs one release, and its build keeps the map fresh.",
		flags: []flagSpec{
			{name: "root", arg: "DIR", help: "project root (default: found from the working directory)"},
			{name: "version", arg: "V", help: "the codemap release (default: this one)"},
			{name: "distribution-url", arg: "URL", help: "where the release archives are, {version} for the release (default: GitHub Releases)"},
		}},
	{name: "config", args: "check|print|schema", about: "Check codemap.yml, print it with the presets applied, or print its schema."},
}

// commandSpec: the entry a query command is checked against (build reaches a query only as --info).
func commandSpec(name string) *command {
	if name == "build" {
		name = "build --info"
	}
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

func (c *command) allFlags() []flagSpec {
	if c.query {
		return append(append([]flagSpec{}, c.flags...), commonFlags...)
	}
	return c.flags
}

func (c *command) hasFlag(name string) bool {
	if strings.HasSuffix(c.name, " --"+name) { // build --info
		return true
	}
	for _, f := range c.allFlags() {
		if f.name == name {
			return true
		}
	}
	return false
}

// maxArgs: how many positionals the command takes, -1 for any number.
func (c *command) maxArgs() int {
	switch {
	case c.args == "":
		return 0
	case strings.HasSuffix(c.args, "..."):
		return -1
	}
	return 1
}

func (f flagSpec) synopsis() string {
	s := "--" + f.name
	if f.arg != "" {
		s += " " + f.arg
	}
	s = "[" + s + "]"
	if f.many {
		s += "..."
	}
	return s
}

// synopsis: the command line wrapped at 100 columns, continuations under the first option.
func (c *command) synopsis() string {
	head := "codemap " + c.name
	if c.args != "" {
		head += " " + c.args
	}
	words := []string{}
	for _, f := range c.allFlags() {
		words = append(words, f.synopsis())
	}
	var b strings.Builder
	b.WriteString(head)
	indent, col := strings.Repeat(" ", len(head)+1), len(head)
	for _, w := range words {
		if col+1+len(w) > 100 {
			b.WriteString("\n" + indent[:len(indent)-1])
			col = len(indent) - 1
		}
		b.WriteString(" " + w)
		col += 1 + len(w)
	}
	return b.String()
}

// Usage: every command on one line, for `codemap` alone and `codemap --help`.
func Usage() string {
	lines := []string{"usage: codemap COMMAND [ARGS] [OPTIONS]", ""}
	for _, c := range commands {
		lines = append(lines, fmt.Sprintf("  %-26s %s", strings.TrimSpace(c.name+" "+c.args), c.about))
	}
	return strings.Join(append(lines, "", "codemap COMMAND --help: the options of a command."), "\n")
}

// Help: the synopsis, what the command does and its options; ok false for an unknown command.
// build has two entries, the build itself and --info.
func Help(name string) (string, bool) {
	var parts []string
	for _, c := range commands {
		if c.name != name && !strings.HasPrefix(c.name, name+" ") {
			continue
		}
		lines := []string{"usage: " + c.synopsis(), "", c.about}
		if len(c.flags) > 0 || c.query {
			lines = append(lines, "")
		}
		for _, f := range c.allFlags() {
			opt := "--" + f.name
			if f.arg != "" {
				opt += " " + f.arg
			}
			if f.many {
				opt += " (repeatable)"
			}
			lines = append(lines, fmt.Sprintf("  %-26s %s", opt, f.help))
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	return strings.Join(parts, "\n\n"), parts != nil
}

// knownFlag: an option of some command, so giving it to another one is a mistake worth naming.
func knownFlag(name string) bool {
	for _, c := range commands {
		if c.hasFlag(name) {
			return true
		}
	}
	return false
}
