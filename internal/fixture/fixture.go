package fixture

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fixtureRoot: testdata/navapp with its classes (classes.zip, made by update.sh) under app/classes.
func FixtureRoot(t *testing.T) string {
	t.Helper()
	src := filepath.Join(TestdataDir(), "navapp")
	root := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		rel, _ := filepath.Rel(src, path)
		if err != nil || d.IsDir() || !(rel == "settings.gradle.kts" || rel == "codemap.yml" ||
			strings.HasPrefix(rel, filepath.Join("app", "src")+string(filepath.Separator))) {
			return err
		}
		data, err := os.ReadFile(path)
		if err == nil {
			os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755)
			err = os.WriteFile(filepath.Join(root, rel), data, 0o644)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(filepath.Join(src, "classes.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		path := filepath.Join(root, "app", "classes", f.Name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(r)
		r.Close()
		os.WriteFile(path, data, 0o644)
	}
	// an old mtime: a map built right after the copy must not see its inputs as edited since
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil {
			os.Chtimes(path, old, old)
		}
		return nil
	})
	return root
}

// testdataDir: the testdata directory at the root of the repository, wherever the test runs.
func TestdataDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata")
}
