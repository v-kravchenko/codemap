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

// version: set by the release build (-ldflags "-X main.version=0.3.0"); empty in other builds.
var version string

// toolVersion: the release build's version, else the module's (go install ...@v0.3.0), else dev.
func toolVersion() string {
	if version != "" {
		return strings.TrimPrefix(version, "v")
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return "dev"
}

func main() {
	query.ToolVersion = toolVersion()
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, query.Usage())
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch {
	case cmd == "--help" || cmd == "-h" || cmd == "help" && len(args) == 0:
		fmt.Println(query.Usage())
		return
	case cmd == "help":
		cmd, args = args[0], []string{"--help"}
	case cmd == "--version" || cmd == "version" && len(args) == 0:
		fmt.Println("codemap " + query.ToolVersion)
		return
	}
	if slices.Contains(args, "--help") || slices.Contains(args, "-h") {
		help, ok := query.Help(cmd)
		if !ok {
			fmt.Fprintf(os.Stderr, "error: unknown command %s\n%s\n", cmd, query.Usage())
			os.Exit(2)
		}
		fmt.Println(help)
		return
	}
	switch cmd {
	case "build":
		if slices.Contains(args, "--info") {
			os.Exit(query.Query("build", args))
		}
		buildCmd(args)
	case "init":
		os.Exit(project.InitCmd(args))
	case "wrapper":
		os.Exit(project.WrapperCmd(args, query.ToolVersion))
	case "config":
		os.Exit(config.ConfigCmd(args))
	case "used-by", "uses", "impact", "find", "changes", "nav-path", "graph", "screens":
		os.Exit(query.Query(cmd, args))
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %s\n%s\n", cmd, query.Usage())
		os.Exit(2)
	}
}

func buildCmd(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	root := fs.String("root", util.FindRoot(), "project root")
	out := fs.String("out", "", "snapshot (default: <variant>.snap in snapshot_dir of codemap.yml)")
	variant := fs.String("variant", "", "build variant (default: variant in codemap.yml)")
	jobs := fs.Int("jobs", 0, "parallel workers (default: CPUs - 1)")
	force := fs.Bool("force", false, "rebuild even when the inputs did not change")
	fs.Parse(args)
	abs, _ := filepath.Abs(*root)
	defer util.Profile()()
	debug.SetGCPercent(400)
	t0 := time.Now()
	c, err := config.LoadConfig(abs)
	if err != nil {
		util.Fail("%v", err)
	}
	if *out == "" {
		if *out, err = config.SnapshotPath(abs, *variant); err != nil {
			util.Fail("%v", err)
		}
	}
	if *variant != "" {
		if err := config.CheckVariant(*variant); err != nil {
			util.Fail("%v", err)
		}
		c.Variant = *variant
	}
	config.ApplyConfig(c)
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
	if err := m.WriteSnapshot(*out); err != nil {
		util.Fail("write %s: %v", *out, err)
	}
	b.Timed("write snapshot", t)
	r, _ := filepath.Rel(abs, *out)
	fmt.Fprintf(os.Stderr, "code map: %d modules, %s classes, %d nodes, %d edges, %d screens, %.1fs -> %s\n",
		len(modules), b.Stats["class_files"], len(b.Nodes), len(b.Edges), len(b.Screens), time.Since(t0).Seconds(), r)
}
