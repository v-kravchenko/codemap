// Command codemap: the code map of an Android project, built from its compiled classes and resources.
package main

import (
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"runtime/pprof"
	"slices"
	"sort"
	"strings"
	"time"
)

const indexVersion = "8"

func relPath(root, p string) (string, error) { return filepath.Rel(root, p) }

func gitOut(root string, args ...string) (string, bool) {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func toolSha1() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return ""
	}
	s := sha1.Sum(data)
	return hex.EncodeToString(s[:])
}

func inputsSha1(b *Builder) string {
	all := map[string]Stamp{}
	for k, v := range b.inputs {
		all[k] = v
	}
	for k, v := range b.sources {
		all[k] = v
	}
	paths := make([]string, 0, len(all))
	for p := range all {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha1.New()
	for _, p := range paths {
		fmt.Fprintf(h, "%s %s\n", p, all[p].Sha1)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func findRoot() string {
	d, _ := os.Getwd()
	for p := d; ; p = filepath.Dir(p) {
		if isFile(filepath.Join(p, "settings.gradle")) || isFile(filepath.Join(p, "settings.gradle.kts")) {
			return p
		}
		if filepath.Dir(p) == p {
			return d
		}
	}
}

const usage = `usage:
  codemap build [--root DIR] [--out SNAPSHOT] [--jobs N] [--force]
  codemap used-by|uses SYMBOL [--all] [--no-self] [--json]
  codemap impact SYMBOL... [--depth N] [--kind KIND] [--no-paths] [--to TARGET] [--nav] [--all] [--json]
  codemap changes [--base REF] [--head REF] [--diff -|FILE] [--files -|FILE] [--nav] [--depth N] [--kind KIND] [--json]
  codemap nav-path SCREEN [--verbose] [--limit N] [--depth N] [--no-paths] [--all] [--json]
  codemap graph SCREEN [--depth N] [--all] [--json]
  codemap screens [FILTER] [--kind KIND] [--json]
  codemap find PATTERN [--all] [--limit N] [--json]
  codemap build --info [--json]
  codemap init [--gradle CMD]
  codemap config check|print|schema`

func defaultSnapshot(root string) string { return snapshotPath(root) }

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "build":
		if slices.Contains(os.Args[2:], "--info") {
			os.Exit(query("build", os.Args[2:]))
		}
		build(os.Args[2:])
	case "init":
		os.Exit(initCmd(os.Args[2:]))
	case "config":
		os.Exit(configCmd(os.Args[2:]))
	case "used-by", "uses", "impact", "find", "changes", "nav-path", "graph", "screens":
		os.Exit(query(os.Args[1], os.Args[2:]))
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}

func build(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	root := fs.String("root", findRoot(), "project root")
	out := fs.String("out", "", "snapshot (default: snapshot in codemap.yml)")
	jobs := fs.Int("jobs", 0, "parallel workers (default: CPUs - 1)")
	force := fs.Bool("force", false, "rebuild even when the inputs did not change")
	fs.Parse(args)
	abs, _ := filepath.Abs(*root)
	if pf := os.Getenv("CODEMAP_PROFILE"); pf != "" {
		f, _ := os.Create(pf)
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	debug.SetGCPercent(400)
	t0 := time.Now()
	c, err := loadConfig(abs)
	if err != nil {
		fail("%v", err)
	}
	applyConfig(c)
	if *out == "" {
		*out = defaultSnapshot(abs)
	}
	var modules []Module
	if pm, err := loadModel(abs); err == nil {
		if modules, err = modelModules(abs, pm, c.Variant); err != nil {
			fail("%v", err)
		}
	} else if !os.IsNotExist(err) {
		fail("%v", err)
	} else {
		modules = discoverModules(abs)
	}
	if len(modules) == 0 {
		fail("No compiled %s classes under %s: compile the %s variant first.", c.Variant, abs, c.Variant)
	}
	if !*force && upToDate(*out, abs, modules) {
		r, _ := filepath.Rel(abs, *out)
		fmt.Fprintf(os.Stderr, "code map is up to date: %s (--force rebuilds it)\n", r)
		return
	}
	b := NewBuilder(abs, modules, *jobs, func(s string) { fmt.Fprintln(os.Stderr, s) })
	b.Build()
	head, _ := gitOut(abs, "rev-parse", "HEAD")
	dirty := "1"
	if s, ok := gitOut(abs, "status", "--porcelain", "--untracked-files=no"); ok && s == "" {
		dirty = "0"
	}
	t := time.Now()
	m := b.Map(map[string]string{"index_version": indexVersion, "tool_sha1": toolSha1(), "git_head": head,
		"dirty": dirty, "inputs_sha1": inputsSha1(b), "built_at": fmt.Sprint(time.Now().Unix()), "root": abs,
		"class_dirs": classDirsJSON(abs, modules), "config": c.JSON(),
		"module_paths": modulePathsJSON(modules)})
	b.timed("columns", t)
	t = time.Now()
	_, capped := m.precomputeOpenedBy(abs, b.jobs)
	b.timed("opened by", t)
	if capped > 0 {
		fmt.Fprintf(os.Stderr, "nav: %d pairs left out: none of their first %d shortest ways passed the host checks\n", capped, pathLimit)
	}
	t = time.Now()
	os.MkdirAll(filepath.Dir(*out), 0o755)
	m.WriteSnapshot(*out)
	b.timed("write snapshot", t)
	r, _ := filepath.Rel(abs, *out)
	fmt.Fprintf(os.Stderr, "code map: %d modules, %s classes, %d nodes, %d edges, %d screens, %.1fs -> %s\n",
		len(modules), b.stats["class_files"], len(b.nodes), len(b.edges), len(b.screens), time.Since(t0).Seconds(), r)
}
