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

// TestChangesBaseDB: with the map of the base, a screen the base's map lacks is new and one the
// head's lacks is removed, by the classes, with no git and no base text.
func TestChangesBaseDB(t *testing.T) {
	root := fixture.FixtureRoot(t)
	run := func(args ...string) string {
		cmd := exec.Command(os.Args[0], args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), "CODEMAP_AS_MAIN=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("codemap %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	classes := filepath.Join(root, "app/classes/navapp/impact/")
	hide := func(name string) func() {
		data, err := os.ReadFile(filepath.Join(classes, name))
		if err != nil {
			t.Fatal(err)
		}
		os.Remove(filepath.Join(classes, name))
		return func() { os.WriteFile(filepath.Join(classes, name), data, 0o644) }
	}
	restore := hide("QuietActivity.class")
	run("build", "--out", "base.snap")
	restore()
	hide("LoudActivity.class")
	run("build")
	dir := "app/src/main/java/navapp/impact/"
	os.WriteFile(filepath.Join(root, "files.txt"), []byte(dir+"Overrides.kt:22\n"), 0o644)
	var a struct {
		Meta struct {
			Refs struct {
				BaseMap struct {
					Commit  *string
					Variant string
				} `json:"base_map"`
			}
		}
		Changed []struct{ Symbol, Status string }
		Screens []struct {
			Name  string
			Added bool
		}
		Removed []struct{ Kind, Name, Target, Module, Location string }
	}
	out := run("changes", "--files", "files.txt", "--base-db", "base.snap", "--json", "--no-check")
	if err := json.Unmarshal([]byte(out), &a); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range a.Changed {
		got[c.Symbol] = c.Status
	}
	for _, s := range a.Screens {
		got["screen "+s.Name] = strconv.FormatBool(s.Added)
	}
	for k, v := range map[string]string{"navapp.impact.QuietActivity.<init>": "added", "screen QuietActivity": "true",
		"screen LoudActivity": ""} {
		if got[k] != v {
			t.Errorf("%s is %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	var removed []string
	for _, r := range a.Removed {
		removed = append(removed, strings.Join([]string{r.Kind, r.Name, r.Target, r.Module, r.Location}, " "))
	}
	if want := []string{"activity LoudActivity navapp.impact.LoudActivity app " + dir + "Overrides.kt:20"}; !slices.Equal(removed, want) {
		t.Errorf("removed\n%v\nwant\n%v", strings.Join(removed, "\n"), strings.Join(want, "\n"))
	}
	if b := a.Meta.Refs.BaseMap; b.Commit != nil || b.Variant != "debug" {
		t.Errorf("meta.refs.base_map: %+v", b)
	}
	if text := run("changes", "--files", "files.txt", "--base-db", "base.snap", "--no-check"); !strings.Contains(text, "Removed: 1") {
		t.Errorf("changes text: no Removed: 1\n%s", text)
	}
}

// TestDiff: a screen only one map has, with its manifest entry, from either side.
func TestDiff(t *testing.T) {
	root := fixture.FixtureRoot(t)
	run := func(args ...string) string {
		cmd := exec.Command(os.Args[0], args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), "CODEMAP_AS_MAIN=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("codemap %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	hide := func(rel string) func() {
		path := filepath.Join(root, "app/classes", rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		os.Remove(path)
		return func() { os.WriteFile(path, data, 0o644) }
	}
	restore := hide("navapp/views/LinkActivity.class")
	run("build", "--out", "base.snap")
	restore()
	hide("navapp/impact/LoudActivity.class")
	run("build")
	type screen struct{ Name, Kind string }
	type entry struct{ Kind, Detail, Screen string }
	type side[T any] struct{ Added, Removed []T }
	var a struct {
		Screens  side[screen]
		Entries  side[entry]
		Warnings []string
	}
	if err := json.Unmarshal([]byte(run("diff", "--base", "base.snap", "--json", "--no-check")), &a); err != nil {
		t.Fatal(err)
	}
	if want := (side[screen]{[]screen{{"LinkActivity", "activity"}}, []screen{{"LoudActivity", "activity"}}}); !slices.Equal(a.Screens.Added, want.Added) || !slices.Equal(a.Screens.Removed, want.Removed) {
		t.Errorf("screens %+v, want %+v", a.Screens, want)
	}
	if len(a.Entries.Added) != 1 || a.Entries.Added[0].Kind != "deeplink" || a.Entries.Added[0].Screen != "LinkActivity" || len(a.Entries.Removed) != 0 {
		t.Errorf("entries %+v, want the deep link of LinkActivity added", a.Entries)
	}
	if len(a.Warnings) != 0 {
		t.Errorf("warnings %v", a.Warnings)
	}
	if text := run("diff", "--base", "base.snap", "--no-check"); !strings.Contains(text, "Screens: +1 -1") || !strings.Contains(text, "+ activity  LinkActivity") {
		t.Errorf("diff text:\n%s", text)
	}
	if text := run("diff", "--base", "base.snap", "--no-check", "--module", "!app"); !strings.Contains(text, "Screens: +0 -0") {
		t.Errorf("diff --module !app:\n%s", text)
	}
}
