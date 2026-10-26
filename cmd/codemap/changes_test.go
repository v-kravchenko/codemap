package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/v-kravchenko/codemap/internal/fixture"
)

// TestChangesBaseIsMergeBase: a branch forks from main, then main moves on; `changes --base main`
// counts only the branch's change, not what landed on main since.
func TestChangesBaseIsMergeBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := fixture.FixtureRoot(t)
	run := func(name string, args ...string) string {
		cmd := exec.Command(name, args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), "CODEMAP_AS_MAIN=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	// edit: a comment at the end of line n, so the line changes and no line moves
	edit := func(rel string, n int) {
		path := filepath.Join(root, "app/src/main/java/navapp/impact", rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(data), "\n")
		lines[n-1] += " // edited"
		os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
	}
	run("git", "init", "-q", "-b", "main")
	run("git", "add", ".")
	run("git", "commit", "-qm", "base")
	run("git", "checkout", "-qb", "feature")
	edit("Overrides.kt", 9)
	run("git", "commit", "-qam", "feature")
	run("git", "checkout", "-q", "main")
	edit("Defaults.kt", 14)
	run("git", "commit", "-qam", "main moves on")
	run("git", "checkout", "-q", "feature")
	run(os.Args[0], "build")
	out := run(os.Args[0], "changes", "--base", "main", "--json", "--no-check")
	changed, _, _ := strings.Cut(out, `"affected"`)
	if !strings.Contains(changed, "navapp.impact.BaseActivity") || strings.Contains(changed, "defaultLabel") {
		t.Errorf("changes --base main: want BaseActivity and not defaultLabel in changed:\n%s", changed)
	}
}

// TestChangesBaseRef: an explicit --base works where none of the default bases exists, and a value
// git would read as an option is refused rather than passed on.
func TestChangesBaseRef(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := fixture.FixtureRoot(t)
	run := func(name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), "CODEMAP_AS_MAIN=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	must := func(name string, args ...string) {
		if out, err := run(name, args...); err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
		}
	}
	must("git", "init", "-q", "-b", "trunk")
	must("git", "add", ".")
	must("git", "commit", "-qm", "base")
	must(os.Args[0], "build")
	if out, err := run(os.Args[0], "changes", "--base", "trunk", "--json", "--no-check"); err != nil {
		t.Errorf("changes --base trunk without main: %v\n%s", err, out)
	}
	for _, args := range [][]string{{"--base", "--show-toplevel"}, {"--head", "--all"}} {
		out, err := run(os.Args[0], append([]string{"changes", "--json", "--no-check"}, args...)...)
		if err == nil || !strings.Contains(out, "is not a git revision") {
			t.Errorf("changes %s: want a refusal, got %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}
