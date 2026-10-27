package query

// Whether the map is older than its inputs: a query warns, or with --strict fails (exit 3) when its answer uses a changed file;
// build does nothing when the map is up to date.

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/v-kravchenko/codemap/internal/build"
	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/project"
	"github.com/v-kravchenko/codemap/internal/snapshot"
	"github.com/v-kravchenko/codemap/internal/util"
)

const exitStale = 3

var staleKinds = [][2]string{{"sources", "source files"}, {"resources", "resource files"}, {"classes", "class files"}}

// staleInputs: the inputs that differ from the ones the map was built from, as (changed, added,
// removed) relative paths, sorted. A file counts as changed when its size or mtime moved and its
// content hash differs too.
func staleInputs(m *snapshot.Map, root string) (changed, added, removed []string) {
	known := map[string]int{}
	for i := range m.Input.Path.Len() {
		known[m.Input.Path.At(i)] = i
	}
	var dirs []string
	json.Unmarshal([]byte(m.MetaValue("class_dirs")), &dirs)
	// a missing module's classes compiled since the build: added; a directory the map has once
	for _, mm := range missingOf(m) {
		for _, d := range mm.ClassDirs {
			if !slices.ContainsFunc(dirs, func(x string) bool { return filepath.ToSlash(x) == d }) {
				dirs = append(dirs, d)
			}
		}
	}
	var modules []project.Module
	json.Unmarshal([]byte(m.MetaValue("module_paths")), &modules)
	lists := make([][]string, len(dirs)+1)
	var wg sync.WaitGroup
	for i, d := range dirs {
		wg.Go(func() {
			filepath.WalkDir(filepath.Join(root, d), func(p string, e os.DirEntry, err error) error {
				if err == nil && !e.IsDir() && strings.HasSuffix(e.Name(), ".class") && e.Name() != "module-info.class" {
					rel, _ := filepath.Rel(root, p)
					lists[i] = append(lists[i], filepath.ToSlash(rel)) // the map's paths use /
				}
				return nil
			})
		})
	}
	wg.Go(func() {
		for _, p := range build.WatchedFiles(root, modules) {
			lists[len(dirs)] = append(lists[len(dirs)], filepath.ToSlash(p))
		}
	})
	wg.Wait()
	var all []string
	for _, l := range lists {
		all = append(all, l...)
	}
	seen := make(map[string]bool, len(all))
	differs := make([]bool, len(all))
	var next sync.Mutex
	pos := 0
	for range runtime.NumCPU() {
		wg.Go(func() {
			for {
				next.Lock()
				i := pos
				pos += 256
				next.Unlock()
				if i >= len(all) {
					return
				}
				for j := i; j < min(i+256, len(all)); j++ {
					k, ok := known[all[j]]
					if ok {
						differs[j] = inputDiffers(m, filepath.Join(root, all[j]), k)
					}
				}
			}
		})
	}
	wg.Wait()
	for i, rel := range all {
		seen[rel] = true
		if _, ok := known[rel]; !ok {
			added = append(added, rel)
		} else if differs[i] {
			changed = append(changed, rel)
		}
	}
	for p := range known {
		if !seen[p] {
			removed = append(removed, p)
		}
	}
	slices.Sort(changed)
	slices.Sort(added)
	slices.Sort(removed)
	return changed, added, removed
}

func inputDiffers(m *snapshot.Map, path string, k int) bool {
	st, err := os.Stat(path)
	if err != nil {
		return true
	}
	if st.Size() == m.Input.Size[k] && st.ModTime().UnixNano() == m.Input.Mtime[k] {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	h := sha1.New()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)) != m.Input.Sha1.At(k)
}

// upToDate: whether the map at `snap` was built by this binary from the same modules and inputs.
func UpToDate(snap, root string, modules, missing []project.Module) bool {
	m, err := snapshot.LoadSnapshot(snap)
	if err != nil {
		return false
	}
	if m.MetaValue("index_version") != snapshot.IndexVersion || m.MetaValue("tool_sha1") != snapshot.ToolSha1() ||
		m.MetaValue("root") != root || m.MetaValue("class_dirs") != project.ClassDirsJSON(root, modules) || m.MetaValue("config") != config.Cfg.JSON() ||
		m.MetaValue("module_paths") != project.ModulePathsJSON(modules) || m.MetaValue("missing_modules") != project.MissingJSON(root, missing) ||
		m.Module.Dir.Len() != len(modules) {
		return false
	}
	for i, mod := range modules {
		if m.Module.Dir.At(i) != filepath.ToSlash(mod.Dir) {
			return false
		}
	}
	c, a, r := staleInputs(m, root)
	return len(c)+len(a)+len(r) == 0
}

type missingModule struct {
	Module    string   `json:"module"`
	ClassDirs []string `json:"class_dirs"`
}

// missingOf: the modules with sources and no classes when the map was built (project.MissingJSON).
func missingOf(m *snapshot.Map) []missingModule {
	var out []missingModule
	json.Unmarshal([]byte(m.MetaValue("missing_modules")), &out)
	return out
}

func (g *Graph) missingNames() []string {
	out := []string{}
	for _, mm := range missingOf(g.m) {
		out = append(out, mm.Module)
	}
	return out
}

// reportMissing: warns of a map without the modules not compiled when it was built: an answer may
// leave out what is in them; with --strict that is an error (exit 3).
func (g *Graph) reportMissing(strict bool, code int) int {
	names := g.missingNames()
	if len(names) == 0 {
		return code
	}
	more := ""
	if len(names) > 5 {
		more = fmt.Sprintf(" and %d more", len(names)-5)
	}
	msg := fmt.Sprintf("the code map misses %d modules (not compiled): %s%s; compile their %s classes, then run `codemap build`",
		len(names), strings.Join(names[:min(5, len(names))], ", "), more, config.Cfg.Variant)
	if strict {
		fmt.Fprintln(os.Stderr, "error: "+msg)
		return exitStale
	}
	fmt.Fprintln(os.Stderr, "warning: "+msg)
	return code
}

// staleInfo: the changed inputs by kind, and the source and resource files they are or belong to (a
// class file: its source).
type staleInfo struct {
	byKind map[string][]string
	files  map[string]bool
}

func (g *Graph) findStale() *staleInfo {
	changed, added, removed := staleInputs(g.m, g.root)
	if len(changed)+len(added)+len(removed) == 0 {
		return nil
	}
	s := &staleInfo{byKind: map[string][]string{}, files: map[string]bool{}}
	var classDirs []string
	json.Unmarshal([]byte(g.m.MetaValue("class_dirs")), &classDirs)
	for _, p := range slices.Concat(changed, added, removed) {
		switch {
		case strings.HasSuffix(p, ".class"):
			s.byKind["classes"] = append(s.byKind["classes"], p)
			p = g.sourceOfClass(p, classDirs)
		case build.IsSource(p):
			s.byKind["sources"] = append(s.byKind["sources"], p)
		default:
			s.byKind["resources"] = append(s.byKind["resources"], p)
		}
		if p != "" {
			s.files[p] = true
		}
	}
	return s
}

func (g *Graph) sourceOfClass(path string, classDirs []string) string {
	for _, d := range classDirs {
		if d = filepath.ToSlash(d); !strings.HasPrefix(path, d+"/") {
			continue
		}
		name := filepath.ToSlash(strings.TrimSuffix(path[len(d)+1:], ".class"))
		c, ok := g.classByName[name]
		if !ok {
			top, _, _ := strings.Cut(name, "$")
			c, ok = g.classByName[top]
		}
		if ok {
			return g.filePath[g.fileOf(c)]
		}
		return ""
	}
	return ""
}

func (g *Graph) moduleDir(path string) (string, bool) {
	best, ok := "", false
	for i := range g.m.Module.Dir.Len() {
		d := g.m.Module.Dir.At(i)
		if (d == "." || strings.HasPrefix(path, d+"/")) && (!ok || len(d) > len(best)) {
			best, ok = d, true
		}
	}
	return best, ok
}

// toCompile: modules with changed sources and no changed class files: not compiled since the edit.
func (g *Graph) toCompile() []string {
	compiled, out := map[string]bool{}, map[string]bool{}
	for _, p := range g.stale.byKind["classes"] {
		if d, ok := g.moduleDir(p); ok {
			compiled[d] = true
		}
	}
	for _, p := range g.stale.byKind["sources"] {
		if d, ok := g.moduleDir(p); ok && !compiled[d] {
			out[d] = true
		}
	}
	return util.SortedKeys(out)
}

// staleHits: the changed files the answer names or walks.
func (g *Graph) staleHits() []string {
	var out []string
	for p := range g.stale.files {
		if id, ok := g.fileID[p]; ok && int(id) < len(g.shown) && g.shown[id] {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

func (g *Graph) staleJSON() any {
	if g.stale == nil {
		return nil
	}
	o := obj{}
	for _, k := range staleKinds {
		o = append(o, kv{k[0], len(g.stale.byKind[k[0]])})
	}
	hits := g.staleHits()
	return append(o, kv{"compile", strs(g.toCompile())}, kv{"affects_answer", len(hits) > 0}, kv{"changed_files", strs(hits)})
}

// reportStale: warns of a map older than its inputs, in detail when the answer uses a changed file;
// with --strict that is an error (exit 3).
func (g *Graph) reportStale(strict bool, code int) int {
	if g.stale == nil {
		return code
	}
	hits := g.staleHits()
	var counts []string
	for _, k := range staleKinds {
		if n := len(g.stale.byKind[k[0]]); n > 0 {
			counts = append(counts, fmt.Sprintf("%d %s", n, k[1]))
		}
	}
	fix := "run `codemap build`"
	if dirs := g.toCompile(); len(dirs) > 0 {
		fix = "compile the " + config.Cfg.Variant + " classes of " + strings.Join(dirs, ", ") + ", then run `codemap build`"
	}
	if len(hits) > 0 {
		more := ""
		if len(hits) > 5 {
			more = fmt.Sprintf(" and %d more", len(hits)-5)
		}
		msg := fmt.Sprintf("the code map is out of date and this answer uses changed files: %s%s (%s changed); %s",
			strings.Join(hits[:min(5, len(hits))], ", "), more, strings.Join(counts, ", "), fix)
		if strict {
			fmt.Fprintln(os.Stderr, "error: "+msg)
			return exitStale
		}
		fmt.Fprintln(os.Stderr, "warning: "+msg)
		return code
	}
	fmt.Fprintf(os.Stderr, "warning: the code map is out of date (%s changed), this answer does not use them; %s\n",
		strings.Join(counts, ", "), fix)
	return code
}
