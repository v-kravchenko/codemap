package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
		"screens --variant beta":                "No code map at " + filepath.Join(root, "build/codemap/beta.snap") + ": run `codemap build --variant beta`",
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

// TestVariantFixedSnapshot: a snapshot path without {variant} holds the configured variant only;
// another one is refused rather than written over it.
func TestVariantFixedSnapshot(t *testing.T) {
	root := fixture.FixtureRoot(t)
	path := filepath.Join(root, "codemap.yml")
	data, _ := os.ReadFile(path)
	os.WriteFile(path, append(data, "snapshot: build/map.snap\n"...), 0o644)
	for _, args := range [][]string{{"build", "--variant", "release"}, {"screens", "--variant", "release"}} {
		cmd := exec.Command(os.Args[0], args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), "CODEMAP_AS_MAIN=1")
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "add {variant} to it") {
			t.Errorf("%s: got %v, want a refusal in\n%s", strings.Join(args, " "), err, out)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "build/map.snap")); err == nil {
		t.Error("build --variant release wrote the configured variant's map")
	}
}
