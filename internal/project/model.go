package project

import (
	"crypto/sha1"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/util"
)

// modelFile: what Gradle says of the build (modules, and for each variant its class, source and
// resource directories), written by `codemap init`. With it the build reads no Gradle file and
// guesses no directory; without it the modules are found by walking the tree (build.class_dirs).
const modelFile = "codemap.model.json"

//go:embed codemap.init.gradle
var initScript []byte

type ProjectModel struct {
	Version   int `json:"version"`
	Generated struct {
		Gradle     string `json:"gradle"`
		InputsSha1 string `json:"inputs_sha1"`
	} `json:"generated"`
	Variants []string      `json:"variants"`
	Modules  []ModelModule `json:"modules"`
}

type ModelModule struct {
	Path     string                  `json:"path"`
	Dir      string                  `json:"dir"`
	Type     string                  `json:"type"`
	Variants map[string]ModelVariant `json:"variants"`
}

type ModelVariant struct {
	SourceSets   []string `json:"source_sets"`
	Classes      []string `json:"classes"`
	CompileTasks []string `json:"compile_tasks"`
	Sources      []string `json:"sources"`
	Res          []string `json:"res"`
	Manifests    []string `json:"manifests"`
}

func LoadModel(root string) (*ProjectModel, error) {
	data, err := os.ReadFile(filepath.Join(root, modelFile))
	if err != nil {
		return nil, err
	}
	var m ProjectModel
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %v", modelFile, err)
	}
	if m.Version != 1 {
		return nil, fmt.Errorf("%s: version %d, this tool reads 1: run `codemap init`", modelFile, m.Version)
	}
	return &m, nil
}

// buildFilesSha1: the hash of what decides the model: settings, build scripts, version catalogs and
// gradle.properties anywhere in the tree, and every file of an included build (build-logic, buildSrc).
func buildFilesSha1(root string) string {
	var paths []string
	var walk func(dir string, included bool)
	walk = func(dir string, included bool) {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		if !included && dir != root && (hasSettings(dir) || filepath.Base(dir) == "buildSrc") {
			included = true
		}
		for _, e := range ents {
			name, p := e.Name(), filepath.Join(dir, e.Name())
			if e.IsDir() {
				if name == "build" || name == "node_modules" || strings.HasPrefix(name, ".") || name == "src" && !included {
					continue
				}
				walk(p, included)
			} else if e.Type().IsRegular() && (included || buildFile(name)) {
				paths = append(paths, p)
			}
		}
	}
	walk(root, false)
	slices.Sort(paths)
	h := sha1.New()
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(root, p)
		s := sha1.Sum(data)
		fmt.Fprintf(h, "%s %x\n", rel, s)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hasSettings(dir string) bool {
	return util.IsFile(filepath.Join(dir, "settings.gradle")) || util.IsFile(filepath.Join(dir, "settings.gradle.kts"))
}

func buildFile(name string) bool {
	return strings.HasSuffix(name, ".gradle") || strings.HasSuffix(name, ".gradle.kts") ||
		strings.HasSuffix(name, ".versions.toml") || name == "gradle.properties"
}

// modelModules: the modules of the model with classes of the variant; the ones not compiled are
// named on stderr with the tasks that compile them.
func ModelModules(root string, m *ProjectModel, variant string) ([]Module, error) {
	if m.Generated.InputsSha1 != buildFilesSha1(root) {
		return nil, fmt.Errorf("the build files changed since `codemap init` wrote %s: run `codemap init`", modelFile)
	}
	if !slices.Contains(m.Variants, variant) {
		return nil, fmt.Errorf("no variant %q in %s (variants: %s): set variant in %s", variant, modelFile,
			strings.Join(m.Variants, ", "), config.ConfigFile)
	}
	mods := slices.Clone(m.Modules)
	slices.SortFunc(mods, func(a, b ModelModule) int { // the order of the tree walk
		return slices.Compare(strings.Split(a.Dir, "/"), strings.Split(b.Dir, "/"))
	})
	var out []Module
	var tasks, missing []string
	for _, mm := range mods {
		v, ok := mm.Variants[variant]
		if !ok {
			continue
		}
		var classes []string
		compiled := false
		for _, c := range v.Classes {
			classes = append(classes, filepath.Join(root, c))
			compiled = compiled || util.IsDir(filepath.Join(root, c))
		}
		if !compiled {
			tasks = append(tasks, v.CompileTasks...)
			missing = append(missing, mm.Path)
			continue
		}
		out = append(out, Module{Name: mm.Dir, Dir: mm.Dir, ClassDirs: classes, Sources: v.Sources, Res: resDirsOf(root, v.Res),
			Manifests: v.Manifests})
	}
	if len(tasks) > 0 {
		run := "./gradlew " + strings.Join(tasks, " ")
		if len(out) == 0 {
			return nil, fmt.Errorf("no compiled %s classes: run %s", variant, run)
		}
		fmt.Fprintf(os.Stderr, "warning: %d modules not compiled, left out of the map: run %s\n", len(missing), run)
	}
	return out, nil
}

// resDirsOf: the resource directories of the model that exist, by name within a source set (res,
// res-common, ...), the source sets in the model's order.
func resDirsOf(root string, dirs []string) []string {
	var out []string
	for _, d := range dirs {
		if util.IsDir(filepath.Join(root, d)) {
			out = append(out, d)
		}
	}
	parents := map[string]int{}
	for _, d := range out {
		if _, ok := parents[filepath.Dir(d)]; !ok {
			parents[filepath.Dir(d)] = len(parents)
		}
	}
	slices.SortStableFunc(out, func(a, b string) int {
		if pa, pb := parents[filepath.Dir(a)], parents[filepath.Dir(b)]; pa != pb {
			return pa - pb
		}
		return strings.Compare(a, b)
	})
	return out
}

// initCmd: codemap init [--gradle CMD]: runs Gradle once (configuration only, nothing compiled) and
// writes ROOT/codemap.model.json.
func InitCmd(args []string) int {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	root := fs.String("root", util.FindRoot(), "project root")
	gradle := fs.String("gradle", os.Getenv("CODEMAP_GRADLE"), "Gradle command (default $CODEMAP_GRADLE, else ./gradlew)")
	fs.Parse(args)
	abs, _ := filepath.Abs(*root)
	if !hasSettings(abs) {
		util.Fail("No settings.gradle under %s: run from the root of a Gradle build.", abs)
	}
	cmdline := strings.Fields(*gradle)
	if len(cmdline) == 0 {
		cmdline = []string{"./gradlew"}
		if !util.IsFile(filepath.Join(abs, "gradlew")) {
			cmdline = []string{"gradle"}
		}
	}
	tmp, err := os.MkdirTemp("", "codemap-init-*")
	if err != nil {
		util.Fail("%v", err)
	}
	defer os.RemoveAll(tmp)
	script, out := filepath.Join(tmp, "codemap.init.gradle"), filepath.Join(tmp, "model.json")
	os.WriteFile(script, initScript, 0o644)
	t0 := time.Now()
	cmd := exec.Command(cmdline[0], append(cmdline[1:], "help", "-q", "--no-configuration-cache",
		"-I", script, "-Pcodemap.out="+out)...)
	cmd.Dir, cmd.Stderr = abs, os.Stderr
	fmt.Fprintf(os.Stderr, "codemap init: %s help -I codemap.init.gradle (configuration only)\n", strings.Join(cmdline, " "))
	if err := cmd.Run(); err != nil {
		util.Fail("Gradle failed: %v. Fix the build or list the class directories in %s (build.class_dirs).", err, config.ConfigFile)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		util.Fail("Gradle wrote no model: %v", err)
	}
	var m ProjectModel
	if err := json.Unmarshal(data, &m); err != nil {
		util.Fail("Gradle wrote a bad model: %v", err)
	}
	m.Generated.InputsSha1 = buildFilesSha1(abs)
	data, _ = json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(abs, modelFile), append(data, '\n'), 0o644); err != nil {
		util.Fail("%v", err)
	}
	fmt.Fprintf(os.Stderr, "codemap init: %d modules, variants %s, %.0fs -> %s\n", len(m.Modules),
		strings.Join(m.Variants, ", "), time.Since(t0).Seconds(), modelFile)
	return 0
}

type Module struct {
	Name, Dir string
	ClassDirs []string // absolute
	// relative to root, in lookup order
	Sources   []string `json:"sources"`
	Res       []string `json:"res"`
	Manifests []string `json:"manifests"`
}

// modulePathsJSON: the sources, resources and manifests of each module, for the stale check.
func ModulePathsJSON(modules []Module) string {
	type paths struct {
		Sources   []string `json:"sources"`
		Res       []string `json:"res"`
		Manifests []string `json:"manifests"`
	}
	out := make([]paths, len(modules))
	for i, m := range modules {
		out[i] = paths{m.Sources, m.Res, m.Manifests}
	}
	data, _ := json.Marshal(out)
	return string(data)
}

func ClassDirsJSON(root string, modules []Module) string {
	set := map[string]bool{}
	for _, m := range modules {
		for _, d := range m.ClassDirs {
			if util.IsDir(d) {
				r, _ := util.RelPath(root, d)
				set[r] = true
			}
		}
	}
	out := util.SortedKeys(set)
	// json.dumps formatting: ", " between items
	parts := make([]string, len(out))
	for i, s := range out {
		bs, _ := json.Marshal(s)
		parts[i] = string(bs)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
