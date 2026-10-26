package main

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func dotted(s string) string { return strings.NewReplacer("/", ".", "$", ".").Replace(s) }

func sortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(2)
}

// userError: a message for the user (exit 2), as codemap.py's SystemExit.
type userError string

func relPath(root, p string) (string, error) { return filepath.Rel(root, p) }

func listDir(dir string) []string {
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

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func findRoot() string {
	d, _ := os.Getwd()
	for p := d; ; p = filepath.Dir(p) {
		if isFile(filepath.Join(p, "settings.gradle")) || isFile(filepath.Join(p, "settings.gradle.kts")) {
			return p
		}
		if filepath.Dir(p) == p {
			return d
		}
	}
}
