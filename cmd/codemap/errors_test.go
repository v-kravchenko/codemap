package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/v-kravchenko/codemap/internal/fixture"
)

// TestErrorJSON: a failed query with --json says on stdout what failed, and why a symbol or file names
// nothing of the map: a module not compiled, a file that is no input, else not in the map.
func TestErrorJSON(t *testing.T) {
	root := fixture.FixtureRoot(t)
	model := `{"version": 1, "variants": ["debug"], "modules": [
	  {"path": ":app", "dir": "app", "variants": {"debug": {"classes": ["app/classes"], "sources": ["app/src/main/java"],
	    "res": ["app/src/main/res"], "manifests": ["app/src/main/AndroidManifest.xml"]}}},
	  {"path": ":lib", "dir": "lib", "variants": {"debug": {"classes": ["lib/classes"], "sources": ["lib/src/main/kotlin"]}}}]}`
	for rel, text := range map[string]string{"build/codemap/gradle-model.json": model, "lib/src/main/kotlin/L.kt": "class L",
		"app/src/test/java/T.kt": "class T"} {
		os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755)
		os.WriteFile(filepath.Join(root, rel), []byte(text), 0o644)
	}
	run := func(args ...string) (string, int) {
		cmd := exec.Command(os.Args[0], args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), "CODEMAP_AS_MAIN=1")
		out, err := cmd.Output()
		code := 0
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		}
		return string(out), code
	}
	if out, code := run("build"); code != 0 {
		t.Fatalf("build: %d\n%s", code, out)
	}
	for _, c := range []struct {
		args                 string
		kind, reason, module string
	}{
		{"impact navapp.Nope", "not_found", "not_in_map", ""},
		{"impact app/src/test/java/T.kt", "not_found", "not_input", ""},
		{"used-by lib/src/main/kotlin/L.kt", "not_found", "not_compiled", "lib"},
		{"screens --module :nope", "not_found", "no_module", ""},
		{"impact navapp.Nope --bogus", "usage", "", ""},
		{"screens --db nope.snap", "map", "", ""},
	} {
		out, code := run(append(strings.Fields(c.args), "--json")...)
		var got struct {
			Command string
			Error   struct {
				Kind, Message  string
				Reason, Module *string
			}
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil || code != 2 {
			t.Errorf("%s: exit %d, %v\n%s", c.args, code, err, out)
			continue
		}
		str := func(p *string) string {
			if p == nil {
				return ""
			}
			return *p
		}
		if e := got.Error; e.Kind != c.kind || str(e.Reason) != c.reason || str(e.Module) != c.module || e.Message == "" ||
			got.Command != strings.Fields(c.args)[0] {
			t.Errorf("%s: got %+v %q/%q, want %s/%s/%s", c.args, e, str(e.Reason), str(e.Module), c.kind, c.reason, c.module)
		}
	}
	if out, code := run("impact", "navapp.Nope"); code != 2 || out != "" {
		t.Errorf("without --json: exit %d, stdout %q", code, out)
	}
}
