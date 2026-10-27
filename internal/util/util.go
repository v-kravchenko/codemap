package util

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"slices"
	"strings"
)

func Set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

var dotted = strings.NewReplacer("/", ".", "$", ".")

func Dotted(s string) string { return dotted.Replace(s) }

func SortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Profile: a CPU profile into $CODEMAP_PROFILE until the returned stop runs.
func Profile() (stop func()) {
	pf := os.Getenv("CODEMAP_PROFILE")
	if pf == "" {
		return func() {}
	}
	f, err := os.Create(pf)
	if err == nil {
		err = pprof.StartCPUProfile(f)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "CODEMAP_PROFILE: %v\n", err)
		return func() {}
	}
	return func() {
		pprof.StopCPUProfile()
		f.Close()
	}
}

func Fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(2)
}

// userError: a message for the user, not a bug (exit 2).
type UserError string

func RelPath(root, p string) (string, error) { return filepath.Rel(root, p) }

func ListDir(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, len(ents))
	for i, e := range ents {
		out[i] = e.Name()
	}
	return out // os.ReadDir sorts by name
}

func IsDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func IsFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// CreateTemp: a new file next to path, to be renamed over it; a name of its own, so two processes
// writing path at once (two builds of the project) do not write one file.
func CreateTemp(path string) (*os.File, error) {
	return os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
}

// WriteFileAtomic: data in a temp file renamed over path, so a reader sees the old or the new file.
func WriteFileAtomic(path string, data []byte) error {
	f, err := CreateTemp(path)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err1 := f.Close(); err == nil {
		err = err1
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

func FindRoot() string {
	d, _ := os.Getwd()
	for p := d; ; p = filepath.Dir(p) {
		if IsFile(filepath.Join(p, "settings.gradle")) || IsFile(filepath.Join(p, "settings.gradle.kts")) {
			return p
		}
		if filepath.Dir(p) == p {
			return d
		}
	}
}
