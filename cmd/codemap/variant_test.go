package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/v-kravchenko/codemap/internal/fixture"
)

// TestVariantMaps: each variant gets a map of its own, the queries find it by --variant and say
// which variant answered.
func TestVariantMaps(t *testing.T) {
	root := fixture.FixtureRoot(t)
	run := func(args ...string) (string, error) {
		cmd := exec.Command(os.Args[0], args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), "CODEMAP_AS_MAIN=1")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	must := func(args ...string) string {
		out, err := run(args...)
		if err != nil {
			t.Fatalf("codemap %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return out
	}
	must("build")
	must("build", "--variant", "release") // the fixture's class_dirs have no {variant}: the same classes
	for _, v := range []string{"debug", "release"} {
		if _, err := os.Stat(filepath.Join(root, "build/codemap", v+".snap")); err != nil {
			t.Errorf("no map of %s: %v", v, err)
		}
	}
	for args, want := range map[string]string{
		"screens --json":                                 `"variant": "debug"`,
		"screens --json --variant release":               `"variant": "release"`,
		"screens --json --db build/codemap/release.snap": `"variant": "release"`,
	} {
		if out := must(strings.Fields(args)...); !strings.Contains(out, want) {
			t.Errorf("%s: want %s in\n%.400s", args, want, out)
		}
	}
	for args, want := range map[string]string{
		"screens --variant beta":                filepath.Join("build", "codemap", "beta.snap") + ": run `codemap build --variant beta`", // the temp root may be a symlink
		"screens --variant release --db x.snap": "not with --db",
		"screens --variant ../x":                "expected a Gradle variant name",
		"build --variant ../x":                  "expected a Gradle variant name",
	} {
		out, err := run(strings.Fields(args)...)
		if err == nil || !strings.Contains(out, want) {
			t.Errorf("%s: got %v, want %q in\n%s", args, err, want, out)
		}
	}
}

// TestSnapshotDir: the maps go into snapshot_dir; the old snapshot key is refused with what replaced it.
func TestSnapshotDir(t *testing.T) {
	root := fixture.FixtureRoot(t)
	path := filepath.Join(root, "codemap.yml")
	data, _ := os.ReadFile(path)
	run := func(extra string, args ...string) (string, error) {
		os.WriteFile(path, append(slices.Clip(data), extra...), 0o644)
		cmd := exec.Command(os.Args[0], args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), "CODEMAP_AS_MAIN=1")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run("snapshot_dir: maps\n", "build", "--variant", "release"); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	if out, err := run("snapshot_dir: maps\n", "screens", "--variant", "release", "--json"); err != nil || !strings.Contains(out, `"variant": "release"`) {
		t.Errorf("screens from maps/release.snap: %v\n%.400s", err, out)
	}
	for _, args := range [][]string{{"build"}, {"screens"}} {
		out, err := run("snapshot: build/map.snap\n", args...)
		if err == nil || !strings.Contains(out, "snapshot is now snapshot_dir") {
			t.Errorf("%s with snapshot: got %v, want a refusal in\n%s", args[0], err, out)
		}
	}
}

// TestMissingModules: a module of the model with sources and no classes is named by the build, warned
// of by every query (an error with --strict), and its classes compiled since make the map stale.
func TestMissingModules(t *testing.T) {
	root := fixture.FixtureRoot(t)
	model := `{"version": 1, "variants": ["debug"], "modules": [
	  {"path": ":app", "dir": "app", "variants": {"debug": {"classes": ["app/classes"], "sources": ["app/src/main/java"],
	    "res": ["app/src/main/res"], "manifests": ["app/src/main/AndroidManifest.xml"]}}},
	  {"path": ":lib", "dir": "lib", "variants": {"debug": {"classes": ["lib/classes"], "sources": ["lib/src/main/kotlin"]}}}]}`
	for rel, text := range map[string]string{"build/codemap/gradle-model.json": model, "lib/src/main/kotlin/L.kt": "class L"} {
		os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755)
		os.WriteFile(filepath.Join(root, rel), []byte(text), 0o644)
	}
	run := func(args ...string) (string, error) {
		cmd := exec.Command(os.Args[0], args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), "CODEMAP_AS_MAIN=1")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run("build"); err != nil || !strings.Contains(out, "code map: 1 of 2 modules compiled") {
		t.Fatalf("build: %v\n%s", err, out)
	}
	if out, err := run("screens", "--json"); err != nil || !strings.Contains(out, `"missing_modules": [
        "lib"
      ]`) || !strings.Contains(out, "warning: the code map misses 1 modules (not compiled): lib;") {
		t.Errorf("screens: %v\n%.1500s", err, out)
	}
	out, err := run("screens", "--strict")
	if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 3 || !strings.Contains(out, "error: the code map misses") ||
		strings.Contains(out, "Screens") {
		t.Errorf("screens --strict: %v\n%.400s", err, out)
	}
	os.MkdirAll(filepath.Join(root, "lib/classes"), 0o755)
	os.WriteFile(filepath.Join(root, "lib/classes/L.class"), nil, 0o644)
	if out, _ := run("screens", "--json"); !strings.Contains(out, `"classes": 1,`) {
		t.Errorf("classes of lib compiled since: not stale\n%.1500s", out)
	}
}

// TestConfigStale: a map built with other settings than codemap.yml gives now is stale for any
// answer; a map of another variant is not stale for its variant alone.
func TestConfigStale(t *testing.T) {
	root := fixture.FixtureRoot(t)
	run := func(args ...string) (string, error) {
		cmd := exec.Command(os.Args[0], args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), "CODEMAP_AS_MAIN=1")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	for _, args := range [][]string{{"build"}, {"build", "--variant", "release"}} {
		if out, err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	if out, _ := run("screens", "--json", "--variant", "release"); !strings.Contains(out, `"stale": false`) {
		t.Errorf("map of release: stale\n%.1500s", out)
	}
	f, _ := os.OpenFile(filepath.Join(root, "codemap.yml"), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("  preview_note: changed\n")
	f.Close()
	if out, _ := run("screens", "--json"); !strings.Contains(out, `"config": true,`) || !strings.Contains(out, `"affects_answer": true,`) ||
		!strings.Contains(out, "warning: the code map was built with other settings") {
		t.Errorf("codemap.yml changed: not stale\n%.1500s", out)
	}
	out, err := run("screens", "--strict")
	if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 3 || strings.Contains(out, "Screens") {
		t.Errorf("screens --strict: %v\n%.400s", err, out)
	}
	run("build")
	if out, _ := run("screens", "--json"); !strings.Contains(out, `"stale": false`) {
		t.Errorf("rebuilt: still stale\n%.1500s", out)
	}
}
