package query

// changes: the impact of base..head, a unified diff or a list of files.

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/v-kravchenko/codemap/internal/android"
	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/util"
)

var (
	ignoredLine = regexp.MustCompile(`^\s*(import\s|package\s|//|/\*|\*|$)`)
	diffGit     = regexp.MustCompile(`^diff --git a/(.*) b/(.*)$`)
	hunkHeader  = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
	fileLineArg = regexp.MustCompile(`^(.*?):(\d+)(?:-(\d+))?$`)
	valueStart  = regexp.MustCompile(`<([\w-]+)\b[^>]*?\bname\s*=\s*"([^"]+)"(?:[^>]*?\btype\s*=\s*"([^"]+)")?`)
)

var notCoveredTitles = [][2]string{{"deleted", "deleted (their callers are analysed when the diff changes them)"},
	{"test_source", "tests"}, {"release_only", "release-only sources (only debug is analysed)"},
	{"not_in_map", "sources the code map has no classes for"},
	{"not_analysed", "build files, manifests and other non-code files"}}

type notCovered struct{ path, reason string }

// lineSet: the changed new-side lines of a file; nil: the whole file (a new one).
type lineSet map[int]bool

func (g *Graph) git(args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", g.root}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		panic(util.UserError(fmt.Sprintf("%s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))))
	}
	return string(out)
}

func (g *Graph) gitOK(args ...string) (string, bool) {
	out, err := exec.Command("git", append([]string{"-C", g.root}, args...)...).Output()
	return string(out), err == nil
}

func (g *Graph) repoPath(name string) string {
	if filepath.IsAbs(name) {
		if r, err := filepath.Rel(g.root, name); err == nil {
			name = r
		}
	}
	return filepath.ToSlash(filepath.Clean(name)) // the map's paths use /
}

func (g *Graph) diffPath(name string) (string, bool) {
	name, _, _ = strings.Cut(name, "\t")
	name = strings.TrimSpace(name)
	if name == "/dev/null" {
		return "", false
	}
	if strings.HasPrefix(name, "a/") || strings.HasPrefix(name, "b/") {
		name = name[2:]
	}
	return g.repoPath(name), true
}

// splitLines: str.splitlines for \n, \r\n and \r.
func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// parseDiff: ({path: new-side lines, nil for a new file}, deleted paths, {moved path: old path}) of a
// unified diff.
func (g *Graph) parseDiff(text string) (map[string]lineSet, map[string]bool, map[string]string) {
	out, deleted, moved := map[string]lineSet{}, map[string]bool{}, map[string]string{}
	var path, oldPath string
	hasPath, hasOld := false, false
	oldLeft, newLeft, newLine := 0, 0, 0
	removed := false
	setdefault := func(p string) {
		if _, ok := out[p]; !ok {
			out[p] = lineSet{}
		}
	}
	mark := func(n int) {
		if hasPath && out[path] != nil {
			out[path][max(n, 1)] = true
		}
	}
	gap := func() {
		mark(newLine - 1)
		mark(newLine)
	}
	for _, line := range splitLines(text) {
		if oldLeft > 0 || newLeft > 0 {
			tag := ""
			if line != "" {
				tag = line[:1]
			}
			switch tag {
			case "+":
				removed = false
				mark(newLine)
				newLine++
				newLeft--
			case "-":
				removed = true
				oldLeft--
			case "\\":
			default:
				if removed {
					gap()
					removed = false
				}
				newLine++
				newLeft--
				oldLeft--
			}
			if removed && oldLeft <= 0 && newLeft <= 0 {
				gap()
				removed = false
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "diff --git "):
			m := diffGit.FindStringSubmatch(line)
			hasPath, hasOld = m != nil, m != nil
			path, oldPath = "", ""
			if m != nil {
				path, oldPath = g.repoPath(m[2]), g.repoPath(m[1])
				if oldPath != path {
					moved[path] = oldPath
				}
				setdefault(path)
			}
		case strings.HasPrefix(line, "new file mode") && hasPath:
			out[path] = nil
		case strings.HasPrefix(line, "deleted file mode") && hasPath:
			deleted[path] = true
		case strings.HasPrefix(line, "--- "):
			oldPath, hasOld = g.diffPath(line[4:])
			hasPath = false
		case strings.HasPrefix(line, "+++ "):
			path, hasPath = g.diffPath(line[4:])
			switch {
			case !hasPath:
				if hasOld {
					deleted[oldPath] = true
					setdefault(oldPath)
				}
			case !hasOld:
				out[path] = nil
			default:
				setdefault(path)
				if oldPath != path {
					moved[path] = oldPath
				}
			}
		case strings.HasPrefix(line, "@@"):
			if m := hunkHeader.FindStringSubmatch(line); m != nil {
				oldLeft = 1
				if m[1] != "" {
					oldLeft, _ = strconv.Atoi(m[1])
				}
				newLine, _ = strconv.Atoi(m[2])
				newLeft = 1
				if m[3] != "" {
					newLeft, _ = strconv.Atoi(m[3])
				}
				if newLeft == 0 {
					newLine++
				}
			}
		}
	}
	return out, deleted, moved
}

// parseFileList: {path: lines, nil for the whole file} of `path`, `path:N` or `path:N-M` lines.
func (g *Graph) parseFileList(text string) map[string]lineSet {
	out := map[string]lineSet{}
	for _, raw := range splitLines(text) {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		m := fileLineArg.FindStringSubmatch(raw)
		if m == nil {
			out[g.repoPath(raw)] = nil
			continue
		}
		path := g.repoPath(m[1])
		start, _ := strconv.Atoi(m[2])
		end := start
		if m[3] != "" {
			end, _ = strconv.Atoi(m[3])
		}
		if lines, ok := out[path]; !ok || lines != nil {
			if !ok {
				lines = lineSet{}
				out[path] = lines
			}
			for n := start; n <= end; n++ {
				lines[n] = true
			}
		}
	}
	return out
}

func readInput(name string) string {
	var data []byte
	var err error
	if name == "-" {
		data, err = io.ReadAll(bufio.NewReader(os.Stdin))
	} else {
		data, err = os.ReadFile(name)
	}
	if err != nil {
		msg := err.Error()
		if pe, ok := err.(*os.PathError); ok {
			msg = pe.Err.Error()
			msg = strings.ToUpper(msg[:1]) + msg[1:]
		}
		panic(util.UserError(fmt.Sprintf("cannot read %s: %s", name, msg)))
	}
	return string(data)
}

// headOfMap: the SHA of `head`; fails unless the map was built from exactly that commit.
func (g *Graph) headOfMap(head string) string {
	sha := strings.TrimSpace(g.git("rev-parse", "--verify", head+"^{commit}"))
	built := g.m.MetaValue("git_head")
	if built == "" {
		built = "?"
	}
	if built != sha {
		panic(util.UserError(fmt.Sprintf("the code map is built from %s, not from --head %s (%s): check it out, compile "+
			"the debug classes and run `codemap build`", built[:min(10, len(built))], head, sha[:min(10, len(sha))])))
	}
	if g.m.MetaValue("dirty") == "1" {
		panic(util.UserError(fmt.Sprintf("the code map is built from a working tree with uncommitted changes, not from "+
			"--head %s: build it from a clean checkout", head)))
	}
	return sha
}

// readAt: a file in revision `rev`, or on disk for ""; ok false when it does not exist.
func (g *Graph) readAt(path, rev string) (string, bool) {
	if rev != "" {
		return g.gitOK("show", rev+":"+path)
	}
	data, err := os.ReadFile(filepath.Join(g.root, path))
	return string(data), err == nil
}

func (g *Graph) cmdChanges(o opts) int {
	if o.diff != "" && o.files != "" {
		panic(util.UserError("--diff and --files cannot be combined"))
	}
	checkRef("--base", o.base)
	checkRef("--head", o.head)
	head := ""
	if o.head != "" {
		head = g.headOfMap(o.head)
	}
	headRef := head
	if headRef == "" {
		headRef = "WORKTREE"
	}
	var linesByPath map[string]lineSet
	deleted, moved := map[string]bool{}, map[string]string{}
	var base any
	baseRev := ""
	if o.diff != "" || o.files != "" {
		// base only gives the old values resources; without it they come from the changed lines
		if o.base != "" {
			baseRev = strings.TrimSpace(g.git("rev-parse", o.base))
			base = baseRev
		}
		input := "files"
		if o.diff != "" {
			input = "diff"
			linesByPath, deleted, moved = g.parseDiff(readInput(o.diff))
		} else {
			linesByPath = g.parseFileList(readInput(o.files))
		}
		g.refs = obj{{"input", input}, {"base", base}, {"head", headRef}}
	} else {
		var ref string
		if o.base != "" {
			ref = g.mergeBase(o.base, head)
		} else {
			ref = g.defaultBase(head)
		}
		baseRev = strings.TrimSpace(g.git("rev-parse", ref))
		args := []string{"diff", "-M", "-U0", baseRev}
		if head != "" {
			args = append(args, head)
		}
		linesByPath, deleted, moved = g.parseDiff(g.git(append(args, "--")...))
		if head == "" {
			for _, f := range splitLines(g.git("ls-files", "--others", "--exclude-standard")) {
				linesByPath[f] = nil
			}
		}
		g.refs = obj{{"input", "git"}, {"base", baseRev}, {"head", headRef}}
	}
	var seeds []Seed
	var nc []notCovered
	var warnings, analysed []string
	for _, path := range util.SortedKeys(linesByPath) {
		lines := linesByPath[path]
		if strings.Contains(path, "/src/test") || strings.Contains(path, "/src/androidTest") {
			nc = append(nc, notCovered{path, "test_source"})
			continue
		}
		if deleted[path] || head == "" && !exists(filepath.Join(g.root, path)) {
			nc = append(nc, notCovered{path, "deleted"})
			continue
		}
		source := strings.HasSuffix(path, ".kt") || strings.HasSuffix(path, ".java")
		var found []Seed
		ok := false
		if source {
			found, ok = g.changedSource(path, lines, head)
		} else if strings.HasSuffix(path, ".xml") && strings.Contains(path, "/res") {
			old, has := moved[path]
			if !has {
				old = path
			}
			found, ok = g.changedResource(path, lines, baseRev, head, old)
		}
		if !ok {
			reason := "not_analysed"
			if strings.Contains(path, "/src/release/") {
				reason = "release_only"
			} else if source {
				reason = "not_in_map"
			}
			nc = append(nc, notCovered{path, reason})
			continue
		}
		seeds = append(seeds, found...)
		if source {
			analysed = append(analysed, path)
		}
	}
	if head == "" {
		builtAt, _ := strconv.ParseInt(g.m.MetaValue("built_at"), 10, 64)
		var newer []string
		for _, p := range analysed {
			if st, err := os.Stat(filepath.Join(g.root, p)); err == nil && float64(st.ModTime().UnixNano())/1e9 > float64(builtAt) {
				newer = append(newer, p)
			}
		}
		if len(newer) > 0 {
			more := ""
			if len(newer) > 10 {
				newer, more = newer[:10], " ..."
			}
			warnings = append(warnings, "edited after the code map was built, its line numbers may not match: "+
				strings.Join(newer, ", ")+more)
		}
	}
	return g.reportImpactWith(sortSeeds(seeds), o, "changes", nc, warnings)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// checkRef: a ref goes to git as an argument, where a leading '-' would read as an option.
func checkRef(from, ref string) {
	if strings.HasPrefix(ref, "-") {
		panic(util.UserError(fmt.Sprintf("%s: %q is not a git revision", from, ref)))
	}
}

func (g *Graph) defaultBase(head string) string {
	if head == "" {
		head = "HEAD"
	}
	for _, ref := range config.Cfg.Changes.Base {
		checkRef("changes.base in "+config.ConfigFile, ref)
		if out, ok := g.gitOK("merge-base", ref, head); ok {
			return strings.TrimSpace(out)
		}
	}
	panic(util.UserError("No base: pass --base REF"))
}

// mergeBase: an explicit --base is a branch the change forked from, as the default is, so what landed
// on it since does not count as changed; a commit on the change's own history is its own merge-base.
func (g *Graph) mergeBase(ref, head string) string {
	if head == "" {
		head = "HEAD"
	}
	out, ok := g.gitOK("merge-base", ref, head)
	if !ok {
		panic(util.UserError(fmt.Sprintf("No common commit of %s and %s: pass another --base", ref, head)))
	}
	return strings.TrimSpace(out)
}

func sortedLines(lines lineSet) []int32 {
	out := make([]int32, 0, len(lines))
	for n := range lines {
		out = append(out, int32(n))
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (g *Graph) changedSource(path string, lines lineSet, head string) ([]Seed, bool) {
	if _, ok := g.fileID[path]; !ok {
		return nil, false
	}
	if lines == nil {
		return g.resolveFile(path, nil), true
	}
	text, ok := g.readAt(path, head)
	if !ok {
		return g.resolveFile(path, nil), true
	}
	rows := strings.Split(text, "\n")
	var meaningful []int32
	for _, ln := range sortedLines(lines) {
		if int(ln)-1 < len(rows) && !ignoredLine.MatchString(rows[ln-1]) {
			meaningful = append(meaningful, ln)
		}
	}
	if len(meaningful) == 0 {
		return []Seed{}, true
	}
	inMethods := g.resolveFile(path, meaningful)
	covered := map[int32]bool{}
	for _, s := range inMethods {
		if s.Mode == modeMember {
			for l := g.m.Node.LMin[s.N]; l <= g.m.Node.LMax[s.N]; l++ {
				covered[l] = true
			}
		}
	}
	for _, ln := range meaningful {
		if !covered[ln] { // declarations outside methods: whole classes
			return sortSeeds(append(append([]Seed{}, inMethods...), g.resolveFile(path, nil)...)), true
		}
	}
	return inMethods, true
}

func (g *Graph) changedResource(path string, lines lineSet, base, head, oldPath string) ([]Seed, bool) {
	if android.ResourceTypeOfDir(filepath.Base(filepath.Dir(path))) == "values" {
		text, _ := g.readAt(path, head)
		var keys []string
		if base != "" {
			oldText, _ := g.readAt(oldPath, base)
			a, b := valuesSignature(oldText), valuesSignature(text)
			for k, v := range a {
				if w, ok := b[k]; !ok || w != v {
					keys = append(keys, k)
				}
			}
			for k := range b {
				if _, ok := a[k]; !ok {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
		} else {
			keys = valuesKeysAtLines(text, lines)
		}
		byName := map[string]int32{}
		for i := g.m.Nodes() - 1; i >= 0; i-- {
			if g.kind(int32(i)) == "R" {
				byName[g.name(int32(i))] = int32(i)
			}
		}
		out := []Seed{}
		for _, k := range keys {
			if n, ok := byName[k]; ok {
				out = append(out, Seed{n, modeWhole})
			}
		}
		return out, true
	}
	if _, ok := g.fileID[path]; ok {
		return g.resolveFile(path, nil), true
	}
	return nil, false
}

// valuesSignature: {resource key: the element normalized} of a values file, to diff two revisions.
func valuesSignature(text string) map[string]string {
	out := map[string]string{}
	root, err := android.ParseXML([]byte(text))
	if err != nil {
		return out
	}
	for _, el := range root.Children {
		name, _ := el.Get("name")
		if name == "" {
			continue
		}
		rtype := android.ValueTags[el.Tag]
		if rtype == "" && el.Tag == "item" {
			rtype, _ = el.Get("type")
		}
		if rtype == "" {
			continue
		}
		var b strings.Builder
		el.Write(&b)
		out[android.ResKey(rtype, name)] = b.String()
	}
	return out
}

// valuesKeysAtLines: the resource keys of the elements a values file has at these lines (an element
// runs from its start tag to the next one); every resource of the file for nil lines.
func valuesKeysAtLines(text string, lines lineSet) []string {
	type start struct {
		no  int
		key string
	}
	var starts []start
	for i, line := range strings.Split(text, "\n") {
		for _, m := range valueStart.FindAllStringSubmatch(line, -1) {
			rtype := android.ValueTags[m[1]]
			if rtype == "" && m[1] == "item" {
				rtype = m[3]
			}
			if rtype != "" && rtype != "id" && rtype != "attr" {
				starts = append(starts, start{i + 1, android.ResKey(rtype, m[2])})
			}
		}
	}
	set := map[string]bool{}
	if lines == nil {
		for _, s := range starts {
			set[s.key] = true
		}
		return util.SortedKeys(set)
	}
	for ln := range lines {
		last := ""
		for _, s := range starts {
			if s.no <= ln {
				last = s.key
			}
		}
		if last != "" {
			set[last] = true
		} else if len(starts) > 0 {
			set[starts[0].key] = true
		}
	}
	return util.SortedKeys(set)
}
