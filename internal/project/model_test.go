package project

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	for rel, text := range files {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBuildFilesSha1(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"settings.gradle": "include ':a'", "a/build.gradle": "android {}", "a/src/main/A.kt": "class A",
		"build-logic/settings.gradle": "", "build-logic/src/main/kotlin/Conv.kt": "class Conv",
	})
	h := buildFilesSha1(root)
	writeFiles(t, root, map[string]string{"a/src/main/A.kt": "class A2", "a/build/x.gradle": "x"})
	if buildFilesSha1(root) != h {
		t.Error("sources and build outputs must not change the hash")
	}
	for rel, text := range map[string]string{"a/build.gradle": "android { flavors }",
		"build-logic/src/main/kotlin/Conv.kt": "class Conv2", "gradle/libs.versions.toml": "[versions]"} {
		writeFiles(t, root, map[string]string{rel: text})
		if buildFilesSha1(root) == h {
			t.Errorf("%s must change the hash", rel)
		}
		h = buildFilesSha1(root)
	}
}

func TestModelModules(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"settings.gradle": "", "b/build.gradle": "", "a/x/build.gradle": "",
		"a/x/build/classes/C.class": "", "b/build/classes/C.class": "", "a-b/build.gradle": "",
		"a-b/src/main/kotlin/A.kt": "", "c/build.gradle": "", "c/build/classes/.keep": "", "c/src/main/kotlin/C.kt": "",
		"d/build.gradle": "", "d/src/main/res/values/v.xml": ""})
	v := func(dir string) map[string]ModelVariant {
		return map[string]ModelVariant{"debug": {Classes: []string{dir + "/build/classes"},
			CompileTasks: []string{":" + dir + ":compileDebugKotlin"}, Sources: []string{dir + "/src/main/kotlin"},
			Res: []string{dir + "/src/main/res"}}}
	}
	m := &ProjectModel{Version: 1, Variants: []string{"debug"}, Modules: []ModelModule{
		{Path: ":b", Dir: "b", Variants: v("b")}, {Path: ":a-b", Dir: "a-b", Variants: v("a-b")},
		{Path: ":a:x", Dir: "a/x", Variants: v("a/x")}, {Path: ":c", Dir: "c", Variants: v("c")},
		{Path: ":d", Dir: "d", Variants: v("d")}}}
	m.Generated.InputsSha1 = buildFilesSha1(root)
	mods, missing, err := ModelModules(root, m, "debug")
	if err != nil {
		t.Fatal(err)
	}
	dirs := func(mods []Module) []string {
		var out []string
		for _, x := range mods {
			out = append(out, x.Dir)
		}
		return out
	}
	if !slices.Equal(dirs(mods), []string{"a/x", "b", "c"}) { // a-b, d not compiled; the tree walk order
		t.Errorf("modules %v", dirs(mods))
	}
	// a-b and c have sources and no class file; d has none to compile
	if !slices.Equal(dirs(missing), []string{"a-b", "c"}) {
		t.Errorf("missing %v", dirs(missing))
	}
	if got := MissingJSON(root, missing); got != `[{"module":"a-b","class_dirs":["a-b/build/classes"]},{"module":"c","class_dirs":["c/build/classes"]}]` {
		t.Errorf("MissingJSON %s", got)
	}
	if _, _, err := ModelModules(root, m, "release"); err == nil || !strings.Contains(err.Error(), `no variant "release"`) {
		t.Errorf("unknown variant: %v", err)
	}
	writeFiles(t, root, map[string]string{"b/build.gradle": "changed"})
	if _, _, err := ModelModules(root, m, "debug"); err == nil || !strings.Contains(err.Error(), "run `codemap init`") {
		t.Errorf("changed build files: %v", err)
	}
}

func TestResDirsOf(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"m/src/main/res-z", "m/src/main/res", "m/src/debug/res"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	got := resDirsOf(root, []string{"m/src/main/res-z", "m/src/main/res", "m/src/main/gone", "m/src/debug/res"})
	if want := []string{"m/src/main/res", "m/src/main/res-z", "m/src/debug/res"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestImportGradleModel(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"settings.gradle": "include ':a'", "a/build.gradle": ""})
	raw := `{"version": 1, "generated": {"gradle": "9.1"}, "variants": ["debug"], "modules": [{"path": ":a", "dir": "a"}]}`
	writeFiles(t, root, map[string]string{gradleModelFile: raw})
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(root, "a/build.gradle"), old, old)
	m, err := LoadModel(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Modules) != 1 || m.Generated.InputsSha1 != buildFilesSha1(root) {
		t.Errorf("imported model %+v", m)
	}
	// a build file changed after the build wrote the model: not imported, the stale check tells
	os.Remove(filepath.Join(root, modelFile))
	writeFiles(t, root, map[string]string{"a/build.gradle": "android {}"})
	later := time.Now().Add(time.Hour)
	os.Chtimes(filepath.Join(root, "a/build.gradle"), later, later)
	if _, err := LoadModel(root); !os.IsNotExist(err) {
		t.Errorf("model of an older configuration imported: %v", err)
	}
}
