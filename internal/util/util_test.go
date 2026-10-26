package util

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestHelpers(t *testing.T) {
	if got := Dotted("com/app/Outer$Inner"); got != "com.app.Outer.Inner" {
		t.Errorf("Dotted: %q", got)
	}
	if got := SortedKeys(map[string]int{"b": 1, "a": 2, "c": 3}); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("SortedKeys: %v", got)
	}
	if s := Set("a", "b"); !s["a"] || !s["b"] || len(s) != 2 {
		t.Errorf("Set: %v", s)
	}
}

func TestFiles(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"b.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	if got := ListDir(dir); !slices.Equal(got, []string{"a.txt", "b.txt", "sub"}) {
		t.Errorf("ListDir: %v", got)
	}
	if ListDir(filepath.Join(dir, "none")) != nil {
		t.Error("ListDir of a missing dir")
	}
	if !IsDir(filepath.Join(dir, "sub")) || IsDir(filepath.Join(dir, "a.txt")) || IsDir(filepath.Join(dir, "none")) {
		t.Error("IsDir")
	}
	if !IsFile(filepath.Join(dir, "a.txt")) || IsFile(filepath.Join(dir, "sub")) || IsFile(filepath.Join(dir, "none")) {
		t.Error("IsFile")
	}
}

// FindRoot: the nearest directory up with a Gradle settings file, else the working directory.
func TestFindRoot(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "app", "src", "main")
	os.MkdirAll(deep, 0o755)
	os.WriteFile(filepath.Join(root, "settings.gradle.kts"), nil, 0o644)
	t.Chdir(deep)
	if got, _ := filepath.EvalSymlinks(FindRoot()); got != mustEval(t, root) {
		t.Errorf("FindRoot: %q, want %q", got, root)
	}
	plain := t.TempDir()
	t.Chdir(plain)
	if got, _ := filepath.EvalSymlinks(FindRoot()); got != mustEval(t, plain) {
		t.Errorf("FindRoot without settings: %q, want %q", got, plain)
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
