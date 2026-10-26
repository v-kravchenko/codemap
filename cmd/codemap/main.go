// Command codemap: the code map of an Android project, built from its compiled classes and resources.
package main

import (
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/v-kravchenko/codemap/internal/build"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"runtime/pprof"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/project"
	"github.com/v-kravchenko/codemap/internal/query"
	"github.com/v-kravchenko/codemap/internal/snapshot"
	"github.com/v-kravchenko/codemap/internal/util"
)

func gitOut(root string, args ...string) (string, bool) {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func inputsSha1(b *build.Builder) string {
	all := map[string]build.Stamp{}
	for k, v := range b.Inputs {
		all[k] = v
	}
	for k, v := range b.Sources {
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

const usage = `usage:
  codemap build [--root DIR] [--out SNAPSHOT] [--jobs N] [--force]
  codemap used-by|uses SYMBOL [--all] [--no-self] [--json]
  codemap impact SYMBOL... [--depth N] [--kind KIND] [--no-paths] [--to TARGET] [--nav] [--all] [--json]
  codemap changes [--base REF] [--head REF] [--diff -|FILE] [--files -|FILE] [--nav] [--depth N] [--kind KIND] [--json]
  codemap nav-path SCREEN [--via SCREEN]... [--ways] [--from SCREEN | --entry launcher|deeplink|intent]
                   [--avoid GLOB]... [--verbose] [--limit N] [--depth N] [--no-paths] [--all] [--json]
  codemap graph SCREEN [--to SCREEN | --in] [--avoid GLOB]... [--module GLOB]... [--depth N] [--all] [--json]
  codemap screens [FILTER] [--kind KIND] [--json]
  codemap find PATTERN [--all] [--limit N] [--json]
  codemap build --info [--json]
  codemap init [--gradle CMD]
  codemap config check|print|schema`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "build":
		if slices.Contains(os.Args[2:], "--info") {
			os.Exit(query.Query("build", os.Args[2:]))
		}
		buildCmd(os.Args[2:])
	case "init":
		os.Exit(project.InitCmd(os.Args[2:]))
	case "config":
		os.Exit(config.ConfigCmd(os.Args[2:]))
	case "used-by", "uses", "impact", "find", "changes", "nav-path", "graph", "screens":
		os.Exit(query.Query(os.Args[1], os.Args[2:]))
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}

func buildCmd(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	root := fs.String("root", util.FindRoot(), "project root")
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
	c, err := config.LoadConfig(abs)
	if err != nil {
		util.Fail("%v", err)
	}
	config.ApplyConfig(c)
	if *out == "" {
		*out = config.DefaultSnapshot(abs)
	}
	var modules []project.Module
	if pm, err := project.LoadModel(abs); err == nil {
		if modules, err = project.ModelModules(abs, pm, c.Variant); err != nil {
			util.Fail("%v", err)
		}
	} else if !os.IsNotExist(err) {
		util.Fail("%v", err)
	} else {
		modules = build.DiscoverModules(abs)
	}
	if len(modules) == 0 {
		util.Fail("No compiled %s classes under %s: compile the %s variant first.", c.Variant, abs, c.Variant)
	}
	if !*force && query.UpToDate(*out, abs, modules) {
		r, _ := filepath.Rel(abs, *out)
		fmt.Fprintf(os.Stderr, "code map is up to date: %s (--force rebuilds it)\n", r)
		return
	}
	b := build.NewBuilder(abs, modules, *jobs, func(s string) { fmt.Fprintln(os.Stderr, s) })
	b.Build()
	head, _ := gitOut(abs, "rev-parse", "HEAD")
	dirty := "1"
	if s, ok := gitOut(abs, "status", "--porcelain", "--untracked-files=no"); ok && s == "" {
		dirty = "0"
	}
	t := time.Now()
	m := b.Map(map[string]string{"index_version": snapshot.IndexVersion, "tool_sha1": snapshot.ToolSha1(), "git_head": head,
		"dirty": dirty, "inputs_sha1": inputsSha1(b), "built_at": fmt.Sprint(time.Now().Unix()), "root": abs,
		"class_dirs": project.ClassDirsJSON(abs, modules), "config": c.JSON(),
		"module_paths": project.ModulePathsJSON(modules)})
	b.Timed("columns", t)
	t = time.Now()
	_, capped := query.PrecomputeOpenedBy(m, abs, b.Jobs)
	b.Timed("opened by", t)
	if capped > 0 {
		fmt.Fprintf(os.Stderr, "nav: %d pairs left out: none of their first %d shortest ways passed the host checks\n", capped, config.PathLimit)
	}
	t = time.Now()
	os.MkdirAll(filepath.Dir(*out), 0o755)
	m.WriteSnapshot(*out)
	b.Timed("write snapshot", t)
	r, _ := filepath.Rel(abs, *out)
	fmt.Fprintf(os.Stderr, "code map: %d modules, %s classes, %d nodes, %d edges, %d screens, %.1fs -> %s\n",
		len(modules), b.Stats["class_files"], len(b.Nodes), len(b.Edges), len(b.Screens), time.Since(t0).Seconds(), r)
}
