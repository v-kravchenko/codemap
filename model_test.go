package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
		"a/x/build/classes/C.class": "", "b/build/classes/C.class": "", "a-b/build.gradle": ""})
	v := func(dir string) map[string]ModelVariant {
		return map[string]ModelVariant{"debug": {Classes: []string{dir + "/build/classes"},
			CompileTasks: []string{":" + dir + ":compileDebugKotlin"}, Res: []string{dir + "/src/main/res"}}}
	}
	m := &ProjectModel{Version: 1, Variants: []string{"debug"}, Modules: []ModelModule{
		{Path: ":b", Dir: "b", Variants: v("b")}, {Path: ":a-b", Dir: "a-b", Variants: v("a-b")},
		{Path: ":a:x", Dir: "a/x", Variants: v("a/x")}}}
	m.Generated.InputsSha1 = buildFilesSha1(root)
	mods, err := modelModules(root, m, "debug")
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, x := range mods {
		dirs = append(dirs, x.Dir)
	}
	if !slices.Equal(dirs, []string{"a/x", "b"}) { // a-b not compiled; the tree walk order
		t.Errorf("modules %v", dirs)
	}
	if _, err := modelModules(root, m, "release"); err == nil || !strings.Contains(err.Error(), `no variant "release"`) {
		t.Errorf("unknown variant: %v", err)
	}
	writeFiles(t, root, map[string]string{"b/build.gradle": "changed"})
	if _, err := modelModules(root, m, "debug"); err == nil || !strings.Contains(err.Error(), "run `codemap init`") {
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
