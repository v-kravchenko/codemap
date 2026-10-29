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

// TestVia: impact's via on navapp/via, with thresholds the fixture's few classes reach (the
// defaults would need tens of screens and messages).
func TestVia(t *testing.T) {
	root := fixture.FixtureRoot(t)
	cfg := filepath.Join(root, "codemap.yml")
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, "nav_path: { hub_min: 3 }\nimpact: { hub_subtypes: 3, base_subclasses: 2 }\n"...)
	if err := os.WriteFile(cfg, data, 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) []byte {
		cmd := exec.Command(os.Args[0], args...)
		cmd.Dir, cmd.Env = root, append(os.Environ(), "CODEMAP_AS_MAIN=1")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("codemap %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return out
	}
	run("build")
	vias := func(args ...string) (map[string]string, map[string]int) {
		var r struct {
			Screens []struct{ Name, Via string }
			Hidden  map[string]int `json:"hidden_by_via"`
		}
		if err := json.Unmarshal(run(append([]string{"impact", "--json"}, args...)...), &r); err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, s := range r.Screens {
			out[s.Name] = s.Via
		}
		return out, r.Hidden
	}
	for _, c := range []struct {
		symbol string
		want   map[string]string
	}{
		{"navapp.via.MessageA", map[string]string{"MessageActivity": "direct", "InboxActivity": "hub"}},
		// a change to the hub itself reaches every screen reading it
		{"navapp.via.MessageRegistry.decode", map[string]string{"InboxActivity": "direct"}},
		{"navapp.via.Flags.getFast", map[string]string{"TargetActivity": "direct", "FirstOpenerActivity": "opener",
			"SecondOpenerActivity": "opener", "ThirdOpenerActivity": "opener"}},
		{"navapp.via.Router.open", map[string]string{"FirstOpenerActivity": "direct"}},
		// code a base runs for every subclass; a change to the base itself is direct
		{"navapp.impact.Session.listen", map[string]string{"LoudActivity": "base"}},
		{"navapp.impact.BaseActivity.observeLogout", map[string]string{"LoudActivity": "direct"}},
	} {
		got, _ := vias(c.symbol)
		for name, want := range c.want {
			if got[name] != want {
				t.Errorf("impact %s: %s via %q, want %q (all: %v)", c.symbol, name, got[name], want, got)
			}
		}
	}
	got, hidden := vias("navapp.via.Flags.getFast", "--direct")
	if len(got) != 1 || got["TargetActivity"] != "direct" || hidden["opener"] != 3 {
		t.Errorf("impact --direct: %v, hidden %v; want TargetActivity alone, 3 openers hidden", got, hidden)
	}
}
