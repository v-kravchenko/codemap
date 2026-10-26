package util

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
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

func Dotted(s string) string { return strings.NewReplacer("/", ".", "$", ".").Replace(s) }

func SortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
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
