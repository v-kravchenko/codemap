package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
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

// TestChangesQualifiers: a color changed only in values-night/ is located there and needs only the
// night render of its screens; changed in values/ too, every configuration.
func TestChangesQualifiers(t *testing.T) {
	root := fixture.FixtureRoot(t)
	res := filepath.Join(root, "app/src/main/res")
	write := func(rel, text string) {
		os.MkdirAll(filepath.Dir(filepath.Join(res, rel)), 0o755)
		if err := os.WriteFile(filepath.Join(res, rel), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	colors := "<resources>\n    <color name=\"pad\">#000</color>\n    <color name=\"accent\">#%s</color>\n</resources>\n"
	write("values/colors.xml", strings.Replace(colors, "%s", "fff", 1))
	write("values-night/colors.xml", strings.Replace(colors, "%s", "111", 1))
	layout, _ := os.ReadFile(filepath.Join(res, "layout/activity_views.xml"))
	write("layout/activity_views.xml", strings.Replace(string(layout), "app:navGraph", "android:background=\"@color/accent\"\n    app:navGraph", 1))
	run := func(args ...string) string {
		cmd := exec.Command(os.Args[0], args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), "CODEMAP_AS_MAIN=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("codemap %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	run("build")
	type answer struct {
		Changed []struct {
			Symbol, Location string
			Qualifiers       []string
		}
		Screens []struct {
			Name       string
			Qualifiers []string
		}
	}
	changes := func(files string) answer {
		os.WriteFile(filepath.Join(root, "files.txt"), []byte(files), 0o644)
		var a answer
		if err := json.Unmarshal([]byte(run("changes", "--files", "files.txt", "--json", "--no-check")), &a); err != nil {
			t.Fatal(err)
		}
		if len(a.Changed) != 1 || a.Changed[0].Symbol != "@color/accent" || len(a.Screens) == 0 {
			t.Fatalf("changes %q: want @color/accent and its screens: %+v", files, a)
		}
		return a
	}
	night := changes("app/src/main/res/values-night/colors.xml:3\n")
	if c := night.Changed[0]; c.Location != "app/src/main/res/values-night/colors.xml:3" || !slices.Equal(c.Qualifiers, []string{"night"}) {
		t.Errorf("night only: %+v, want values-night/colors.xml:3 and [night]", c)
	}
	for _, s := range night.Screens {
		if !slices.Equal(s.Qualifiers, []string{"night"}) {
			t.Errorf("night only: screen %s qualifiers %v, want [night]", s.Name, s.Qualifiers)
		}
	}
	both := changes("app/src/main/res/values/colors.xml:3\napp/src/main/res/values-night/colors.xml:3\n")
	if c := both.Changed[0]; c.Location != "app/src/main/res/values/colors.xml:3" || c.Qualifiers == nil || len(c.Qualifiers) != 0 {
		t.Errorf("both: %+v, want values/colors.xml:3 and []", c)
	}
	for _, s := range both.Screens {
		if s.Qualifiers != nil {
			t.Errorf("both: screen %s qualifiers %v, want null", s.Name, s.Qualifiers)
		}
	}
}

// TestChangesDiffMismatch: a diff whose lines are not those of the map's sources (another
// revision) is answered with a warning, and refused with --strict.
func TestChangesDiffMismatch(t *testing.T) {
	root := fixture.FixtureRoot(t)
	run := func(args ...string) (string, int) {
		cmd := exec.Command(os.Args[0], args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), "CODEMAP_AS_MAIN=1")
		out, _ := cmd.CombinedOutput()
		return string(out), cmd.ProcessState.ExitCode()
	}
	run("build")
	rel := "app/src/main/java/navapp/impact/Overrides.kt"
	data, _ := os.ReadFile(filepath.Join(root, rel))
	line := strings.Split(string(data), "\n")[8]
	hunk := func(added string) {
		diff := "--- a/" + rel + "\n+++ b/" + rel + "\n@@ -9 +9 @@\n-old\n+" + added + "\n"
		os.WriteFile(filepath.Join(root, "mr.diff"), []byte(diff), 0o644)
	}
	const warn = "the diff does not match the sources of the code map"
	hunk(line)
	if out, code := run("changes", "--diff", "mr.diff", "--json", "--no-check", "--strict"); code != 0 || strings.Contains(out, warn) {
		t.Errorf("a diff of the map's sources: exit %d\n%s", code, out)
	}
	hunk(line + " // another revision")
	if out, code := run("changes", "--diff", "mr.diff", "--json", "--no-check"); code != 0 || !strings.Contains(out, warn+", its lines may name other symbols: "+rel) {
		t.Errorf("a diff of another revision: want a warning, exit %d\n%s", code, out)
	}
	if out, code := run("changes", "--diff", "mr.diff", "--no-check", "--strict"); code != 3 || !strings.Contains(out, "error: "+warn) {
		t.Errorf("a diff of another revision, --strict: want exit 3, got %d\n%s", code, out)
	}
}

// TestChangesRemoved: the screens the base's text declares and the head has not, from a deleted
// file and from one that lost a class; a --diff alone has the deleted file's text only, --files
// without --base nothing.
func TestChangesRemoved(t *testing.T) {
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
	dir := "app/src/main/java/navapp/impact/"
	gone := "package navapp.impact\n\nimport androidx.compose.runtime.Composable\nimport navapp.sections.Preview\n\n" +
		"class GoneActivity : BaseActivity() {\n    override fun observeLogout() = Unit\n}\n\n" +
		"@Preview\n@Composable\nfun GonePreview() = Unit\n\n@Composable\nfun GoneContent() = Unit\n"
	overrides, _ := os.ReadFile(filepath.Join(root, dir+"Overrides.kt"))
	os.WriteFile(filepath.Join(root, dir+"Gone.kt"), []byte(gone), 0o644)
	os.WriteFile(filepath.Join(root, dir+"Overrides.kt"), append(slices.Clone(overrides), "\nclass OldActivity(x: Int) : navapp.impact.BaseActivity()\n"...), 0o644)
	run("git", "init", "-q", "-b", "main")
	run("git", "add", ".")
	run("git", "commit", "-qm", "base")
	os.Remove(filepath.Join(root, dir+"Gone.kt"))
	os.WriteFile(filepath.Join(root, dir+"Overrides.kt"), overrides, 0o644)
	run("git", "commit", "-qam", "remove screens")
	run(os.Args[0], "build")
	removed := func(args ...string) []string {
		var a struct {
			Removed *[]struct{ Kind, Name, Target, Module, Location string }
		}
		if err := json.Unmarshal([]byte(run(os.Args[0], append([]string{"changes", "--json", "--no-check"}, args...)...)), &a); err != nil {
			t.Fatal(err)
		}
		if a.Removed == nil {
			return nil
		}
		out := []string{}
		for _, r := range *a.Removed {
			out = append(out, strings.Join([]string{r.Kind, r.Name, r.Target, r.Module, r.Location}, " "))
		}
		return out
	}
	gonePreview := "preview Gone.GonePreview navapp.impact.GoneKt.GonePreview app " + dir + "Gone.kt:12"
	goneActivity := "activity GoneActivity navapp.impact.GoneActivity app " + dir + "Gone.kt:6"
	oldActivity := "activity OldActivity navapp.impact.OldActivity app " + dir + "Overrides.kt:" + strconv.Itoa(strings.Count(string(overrides), "\n")+2)
	if got, want := removed("--base", "HEAD~1"), []string{goneActivity, oldActivity, gonePreview}; !slices.Equal(got, want) {
		t.Errorf("changes --base HEAD~1: removed\n%v\nwant\n%v", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	os.WriteFile(filepath.Join(root, "mr.diff"), []byte(run("git", "diff", "HEAD~1", "HEAD")), 0o644)
	if got, want := removed("--diff", "mr.diff"), []string{goneActivity, gonePreview}; !slices.Equal(got, want) {
		t.Errorf("changes --diff alone: removed %v, want %v", got, want)
	}
	os.WriteFile(filepath.Join(root, "files.txt"), []byte(dir+"Gone.kt\n"), 0o644)
	if got := removed("--files", "files.txt"); got != nil {
		t.Errorf("changes --files without --base: removed %v, want null", got)
	}
}
