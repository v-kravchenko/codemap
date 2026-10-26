package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/v-kravchenko/codemap/internal/fixture"
)

var update = flag.Bool("update", false, "rewrite testdata/golden from the current output")

// TestMain runs the test binary as the codemap command when CODEMAP_AS_MAIN is set, so the golden
// tests drive the real CLI without building it.
func TestMain(m *testing.M) {
	if os.Getenv("CODEMAP_AS_MAIN") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// goldenCalls: the queries whose --json is kept in testdata/golden. The impact calls are the
// precision rules of the walk (testdata/navapp/app/src/main/java/navapp/impact).
var goldenCalls = [][]string{
	{"impact", "navapp.impact.BaseActivity.observeLogout"}, // an override cuts QuietActivity
	{"impact", "navapp.impact.defaultLabel"},               // a default argument left out
	{"impact", "navapp.impact.FirstActivity.<init>"},       // a constructor overrides nothing
	{"impact", "navapp.impact.CounterActivity.getCount"},
	{"impact", "navapp.impact.CartViewModel.total"}, // a ViewModel through a generic base
	{"impact", "navapp.impact.MAX_ITEMS"},           // a const val, found in the sources
	{"impact", "navapp.impact.Profile.getNickname"}, // generated copy/equals do not spread it
	{"impact", "navapp.impact.Session.listen"},
	{"impact", "navapp.typed.SettingsViewModel.openSettings"},
	{"used-by", "navapp.impact.defaultLabel"},
	{"uses", "navapp.impact.PlainCopyActivity.onCreate"},
	{"find", "Activity"},
	{"screens"},
	{"nav-path", "AboutFragment"},
	{"nav-path", "LicensesActivity"},
	{"nav-path", "InfoDialog"},
	{"nav-path", "Login"},
	{"nav-path", "comments/{postId}"},
	{"nav-path", "Message"},
	{"nav-path", "Item"},
	{"nav-path", "Settings", "--via", "Profile"},
	{"nav-path", "Settings", "--via", "Login"},
	{"nav-path", "InfoDialog", "--ways"},
	{"nav-path", "InfoDialog", "--entry", "deeplink"},
	{"nav-path", "InfoDialog", "--from", "ListFragment"},
	{"nav-path", "InfoDialog", "--avoid", "Main*"},
	{"graph", "ViewsActivity"},
	{"graph", "Home"},
	{"graph", "MainActivity", "--to", "InfoDialog"},
	{"graph", "MainActivity", "--to", "Settings", "--depth", "1"},
	{"graph", "MainActivity", "--depth", "3", "--avoid", "Home"},
	{"graph", "MainActivity", "--depth", "2", "--module", "feature/*"},
	{"graph", "InfoDialog", "--in", "--depth", "5"},
	{"changes", "--files", "changed.txt"},
}

// TestGolden: the --json of goldenCalls on the fixture, without the run's meta, against
// testdata/golden (go test -run TestGolden -update rewrites them).
func TestGolden(t *testing.T) {
	root := fixture.FixtureRoot(t)
	changed := "app/src/main/java/navapp/impact/Overrides.kt\napp/src/main/res/navigation/views_graph.xml\n"
	if err := os.WriteFile(filepath.Join(root, "changed.txt"), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	os.Chtimes(filepath.Join(root, "changed.txt"), old, old)
	run := func(args ...string) []byte {
		cmd := exec.Command(os.Args[0], args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), "CODEMAP_AS_MAIN=1")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("codemap %s: %v\n%s", strings.Join(args, " "), err, stderr.Bytes())
		}
		return out
	}
	run("build")
	seen := map[string]bool{}
	for _, call := range goldenCalls {
		name := goldenName(call)
		if seen[name] {
			t.Fatalf("two calls write %s", name)
		}
		seen[name] = true
		got := withoutMeta(run(append(call, "--json")...))
		path := filepath.Join(fixture.TestdataDir(), "golden", name)
		if *update {
			os.MkdirAll(filepath.Dir(path), 0o755)
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v (go test -run TestGolden -update writes it)", name, err)
		} else if !bytes.Equal(got, want) {
			t.Errorf("codemap %s --json differs from %s at %s (go test -run TestGolden -update rewrites it; check the diff)",
				strings.Join(call, " "), path, firstDiff(got, want))
		}
	}
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func goldenName(call []string) string {
	return strings.Trim(unsafeName.ReplaceAllString(strings.Join(call, "_"), "-"), "-") + ".json"
}

// withoutMeta drops the top-level "meta" object: times, hashes and the git state of the run.
func withoutMeta(out []byte) []byte {
	var kept []string
	skip := false
	for _, line := range strings.SplitAfter(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, `  "meta": {`):
			skip = true
		case skip:
			skip = !strings.HasPrefix(line, "  }")
		default:
			kept = append(kept, line)
		}
	}
	return []byte(strings.Join(kept, ""))
}

// firstDiff: the first line where got and want differ, with its number.
func firstDiff(got, want []byte) string {
	g, w := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
	for i := range max(len(g), len(w)) {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return fmt.Sprintf("line %d: got %q, want %q", i+1, gl, wl)
		}
	}
	return "the end"
}
