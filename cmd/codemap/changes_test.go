package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/v-kravchenko/codemap/internal/fixture"
)

// gitTestEnv: an identity for the commits, and no auto gc or maintenance, whose detached process
// may still write to .git when the test removes its temp dir ("directory not empty", CI flake).
var gitTestEnv = []string{"CODEMAP_AS_MAIN=1",
	"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	"GIT_CONFIG_COUNT=3", "GIT_CONFIG_KEY_0=gc.auto", "GIT_CONFIG_VALUE_0=0",
	"GIT_CONFIG_KEY_1=maintenance.auto", "GIT_CONFIG_VALUE_1=false",
	"GIT_CONFIG_KEY_2=core.fsmonitor", "GIT_CONFIG_VALUE_2=false"}

// removeGitLate: .git removed with retries before the temp dir goes, for a git process that
// still writes there after the test.
func removeGitLate(t *testing.T, root string) {
	t.Cleanup(func() {
		for range 20 {
			if os.RemoveAll(filepath.Join(root, ".git")) == nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
}

// TestChangesBaseIsMergeBase: a branch forks from main, then main moves on; `changes --base main`
// counts only the branch's change, not what landed on main since.
func TestChangesBaseIsMergeBase(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := fixture.FixtureRoot(t)
	removeGitLate(t, root)
	run := func(name string, args ...string) string {
		cmd := exec.Command(name, args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), gitTestEnv...)
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
	removeGitLate(t, root)
	run := func(name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), gitTestEnv...)
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

// TestChangesStatus: a class the base has not is added and its screen new; what the base has is
// modified, its screens changed or affected; --files without --base cannot tell.
func TestChangesStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := fixture.FixtureRoot(t)
	removeGitLate(t, root)
	run := func(name string, args ...string) string {
		cmd := exec.Command(name, args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), gitTestEnv...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	path := filepath.Join(root, "app/src/main/java/navapp/impact/Overrides.kt")
	data, _ := os.ReadFile(path)
	quiet := "class QuietActivity : BaseActivity() {\n    override fun observeLogout() = Unit\n}\n\n"
	os.WriteFile(path, []byte(strings.Replace(string(data), quiet, "", 1)), 0o644)
	run("git", "init", "-q", "-b", "main")
	run("git", "add", ".")
	run("git", "commit", "-qm", "base")
	os.WriteFile(path, data, 0o644)
	run("git", "commit", "-qam", "add QuietActivity")
	run(os.Args[0], "build")
	type item struct{ Symbol, Name, Status string }
	var got struct {
		Changed []struct {
			Symbol string
			Status *string
		}
		Screens []item
	}
	statuses := func(args ...string) map[string]string {
		out := run(os.Args[0], append([]string{"changes", "--json", "--no-check"}, args...)...)
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for _, c := range got.Changed {
			m[c.Symbol] = "null"
			if c.Status != nil {
				m[c.Symbol] = *c.Status
			}
		}
		for _, s := range got.Screens {
			m["screen "+s.Name] = s.Status
		}
		return m
	}
	want := map[string]string{"navapp.impact.QuietActivity": "added", "navapp.impact.BaseActivity": "modified",
		"screen QuietActivity": "new", "screen LoudActivity": "changed"}
	diff := run("git", "diff", "HEAD~1", "HEAD")
	os.WriteFile(filepath.Join(root, "mr.diff"), []byte(diff), 0o644)
	for _, args := range [][]string{{"--base", "HEAD~1"}, {"--diff", "mr.diff", "--base", "HEAD~1"}} {
		got := statuses(args...)
		for k, v := range want {
			if got[k] != v {
				t.Errorf("changes %s: %s is %q, want %q (all: %v)", strings.Join(args, " "), k, got[k], v, got)
			}
		}
	}
	// no base: a function all of whose lines are added; a class is modified, its screen changed
	if got := statuses("--diff", "mr.diff"); got["navapp.impact.QuietActivity"] != "modified" ||
		got["screen QuietActivity"] != "changed" {
		t.Errorf("changes --diff without --base: %v", got)
	}
	os.WriteFile(filepath.Join(root, "files.txt"), []byte("app/src/main/java/navapp/impact/Overrides.kt:22\n"), 0o644)
	if got := statuses("--files", "files.txt"); got["navapp.impact.QuietActivity.<init>"] != "null" {
		t.Errorf("changes --files: %v", got)
	}
}
