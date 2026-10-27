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

// gradleModelFile: the model as the hook of a project's build writes it on each configuration
// (codemap.gradle); LoadModel takes it over when it is newer than the build files.
const gradleModelFile = "build/codemap/gradle-model.json"

// GradleScript: both the init script of `codemap init` and the hook `codemap wrapper` writes.
//
//go:embed codemap.gradle
var GradleScript []byte

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
	// the Gradle paths of the modules it depends on; nil in a model of a script before them (a
	// hook's older copy): every module then counts
	CompileDeps []string `json:"compile_deps"`
	RuntimeDeps []string `json:"runtime_deps"`
}

func LoadModel(root string) (*ProjectModel, error) {
	if err := importGradleModel(root); err != nil {
		return nil, err
	}
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

// importGradleModel: the model the build's hook wrote becomes codemap.model.json when it is newer
// than that and than every build file; one older than a build file is of a configuration before
// the change, and the stale check of ModelModules says so.
func importGradleModel(root string) error {
	raw, err := os.Stat(filepath.Join(root, gradleModelFile))
	if err != nil {
		return nil
	}
	if m, err := os.Stat(filepath.Join(root, modelFile)); err == nil && !raw.ModTime().After(m.ModTime()) {
		return nil
	}
	for _, p := range buildFiles(root) {
		if f, err := os.Stat(p); err == nil && f.ModTime().After(raw.ModTime()) {
			return nil
		}
	}
	data, err := os.ReadFile(filepath.Join(root, gradleModelFile))
	if err != nil {
		return err
	}
	_, err = writeModel(root, data)
	return err
}

// writeModel: the model from Gradle with the hash of the build files it was made from.
func writeModel(root string, data []byte) (*ProjectModel, error) {
	var m ProjectModel
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("a bad model from Gradle: %v", err)
	}
	m.Generated.InputsSha1 = buildFilesSha1(root)
	data, _ = json.MarshalIndent(m, "", "  ")
	// a temp file renamed over the model: two builds of the project may import it at once
	return &m, util.WriteFileAtomic(filepath.Join(root, modelFile), append(data, '\n'))
}

// buildFilesSha1: the hash of what decides the model (buildFiles).
func buildFilesSha1(root string) string {
	h := sha1.New()
	for _, p := range buildFiles(root) {
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

// buildFiles: settings, build scripts, version catalogs and gradle.properties anywhere in the tree,
// and every file of an included build (build-logic, buildSrc), sorted.
func buildFiles(root string) []string {
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
	return paths
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
// ModelModules: the modules of the variant with class directories, and the missing ones: with sources
// and no class file, not compiled (in the map, with an empty class directory, or left out).
func ModelModules(root string, m *ProjectModel, variant string) ([]Module, []Module, error) {
	if m.Generated.InputsSha1 != buildFilesSha1(root) {
		return nil, nil, fmt.Errorf("the build files changed since `codemap init` wrote %s: run `codemap init`", modelFile)
	}
	if !slices.Contains(m.Variants, variant) {
		return nil, nil, fmt.Errorf("no variant %q in %s (variants: %s): set variant in %s", variant, modelFile,
			strings.Join(m.Variants, ", "), config.ConfigFile)
	}
	mods := slices.Clone(m.Modules)
	slices.SortFunc(mods, func(a, b ModelModule) int { // the order of the tree walk
		return slices.Compare(strings.Split(a.Dir, "/"), strings.Split(b.Dir, "/"))
	})
	inApp, compiledBy := appGraph(mods, variant)
	var out, missing []Module
	var tasks, left []string
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
		mod := Module{Name: mm.Dir, Dir: mm.Dir, ClassDirs: classes, Sources: v.Sources, Res: resDirsOf(root, v.Res),
			Manifests: v.Manifests}
		// a module no app has (a test helper) misses nothing of the app's map
		app := inApp == nil || inApp[mm.Path]
		if app && !slices.ContainsFunc(classes, func(d string) bool { return hasFile(d, ".class") }) &&
			slices.ContainsFunc(v.Sources, func(d string) bool { return hasFile(filepath.Join(root, d), ".kt", ".java") }) {
			missing = append(missing, mod)
		}
		if !compiled {
			if app {
				left = append(left, mm.Path)
				if by := compiledBy[mm.Path]; len(by) > 0 {
					tasks = append(tasks, by...)
				} else {
					tasks = append(tasks, v.CompileTasks...)
				}
			}
			continue
		}
		out = append(out, mod)
	}
	if len(tasks) > 0 {
		var once []string // an app's tasks once, in the order first named
		for _, t := range tasks {
			if !slices.Contains(once, t) {
				once = append(once, t)
			}
		}
		run := "./gradlew " + strings.Join(once, " ")
		if len(out) == 0 {
			return nil, nil, fmt.Errorf("no compiled %s classes: run %s", variant, run)
		}
		fmt.Fprintf(os.Stderr, "warning: %d modules not compiled, left out of the map: run %s\n", len(left), run)
	}
	return out, missing, nil
}

// appGraph: the modules of the apps (application and dynamic-feature modules and what they depend
// on), and for each the compile tasks of the first app whose compilation compiles it too (reached by
// compile dependencies alone). nil, nil when the model has no app or no dependencies (an older
// script): every module counts and compiles by its own tasks.
func appGraph(mods []ModelModule, variant string) (map[string]bool, map[string][]string) {
	byPath := map[string]ModelVariant{}
	var apps []ModelModule
	for _, mm := range mods {
		v, ok := mm.Variants[variant]
		if !ok {
			continue
		}
		if v.CompileDeps == nil || v.RuntimeDeps == nil {
			return nil, nil
		}
		byPath[mm.Path] = v
		if mm.Type == "application" || mm.Type == "dynamic-feature" {
			apps = append(apps, mm)
		}
	}
	if len(apps) == 0 {
		return nil, nil
	}
	walk := func(from string, deps func(ModelVariant) []string) []string {
		seen, queue := map[string]bool{from: true}, []string{from}
		for len(queue) > 0 {
			x := queue[0]
			queue = queue[1:]
			for _, d := range deps(byPath[x]) {
				if _, ok := byPath[d]; ok && !seen[d] {
					seen[d] = true
					queue = append(queue, d)
				}
			}
		}
		return util.SortedKeys(seen)
	}
	inApp, compiledBy := map[string]bool{}, map[string][]string{}
	for _, a := range apps {
		for _, p := range walk(a.Path, func(v ModelVariant) []string { return slices.Concat(v.CompileDeps, v.RuntimeDeps) }) {
			inApp[p] = true
		}
		for _, p := range walk(a.Path, func(v ModelVariant) []string { return v.CompileDeps }) {
			if _, ok := compiledBy[p]; !ok { // one app's tasks are enough
				compiledBy[p] = byPath[a.Path].CompileTasks
			}
		}
	}
	return inApp, compiledBy
}

// hasFile: whether the tree under dir has a file with one of the suffixes; stops at the first.
func hasFile(dir string, suffixes ...string) bool {
	found := false
	filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() && slices.ContainsFunc(suffixes, func(s string) bool { return strings.HasSuffix(e.Name(), s) }) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
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
	// util.Fail exits, skipping the deferred cleanup
	fail := func(format string, a ...any) {
		os.RemoveAll(tmp)
		util.Fail(format, a...)
	}
	script, out := filepath.Join(tmp, "codemap.gradle"), filepath.Join(tmp, "model.json")
	if err := os.WriteFile(script, GradleScript, 0o644); err != nil {
		fail("%v", err)
	}
	t0 := time.Now()
	cmd := exec.Command(cmdline[0], append(cmdline[1:], "help", "-q", "--no-configuration-cache",
		"-I", script, "-Pcodemap.out="+out)...)
	cmd.Dir, cmd.Stderr = abs, os.Stderr
	fmt.Fprintf(os.Stderr, "codemap init: %s help -I codemap.gradle (configuration only)\n", strings.Join(cmdline, " "))
	if err := cmd.Run(); err != nil {
		fail("Gradle failed: %v. Fix the build or list the class directories in %s (build.class_dirs).", err, config.ConfigFile)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		fail("Gradle wrote no model: %v", err)
	}
	m, err := writeModel(abs, data)
	if err != nil {
		fail("%v", err)
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

// MissingJSON: the missing modules (ModelModules) and their class directories, sorted: the stale check
// of a query looks there for classes compiled since.
func MissingJSON(root string, missing []Module) string {
	type mod struct {
		Module    string   `json:"module"`
		ClassDirs []string `json:"class_dirs"`
	}
	out := []mod{}
	for _, m := range missing {
		x := mod{Module: m.Name, ClassDirs: []string{}}
		for _, d := range m.ClassDirs {
			r, _ := util.RelPath(root, d)
			x.ClassDirs = append(x.ClassDirs, filepath.ToSlash(r))
		}
		out = append(out, x)
	}
	slices.SortFunc(out, func(a, b mod) int { return strings.Compare(a.Module, b.Module) })
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
