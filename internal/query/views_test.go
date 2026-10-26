package query

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/v-kravchenko/codemap/internal/build"
	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/fixture"
)

// TestViewsNavigation: who opens the fragments of an XML navigation graph (its start through the
// host's layout, an <action>, a nested graph, a dialog) and an Activity started with an Intent.
func TestViewsNavigation(t *testing.T) {
	root := fixture.FixtureRoot(t)
	c, err := config.LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	config.ApplyConfig(c)
	t.Cleanup(func() {
		d, _ := config.LoadConfig(t.TempDir())
		config.ApplyConfig(d)
	})
	b := build.NewBuilder(root, build.DiscoverModules(root), 2, func(string) {})
	b.Build()
	m := b.Map(map[string]string{"root": root})
	PrecomputeOpenedBy(m, root, 2)
	g := NewGraph(m, root)

	got := map[string][]string{}
	for sid := range g.screens {
		name := g.screenName(sid)
		if !strings.HasPrefix(g.qualified(sid), "navapp.views.") {
			continue
		}
		ways, _ := g.openedBy(sid)
		var srcs []string
		for _, w := range ways {
			via := []string{g.screenName(w.Src)}
			for _, n := range w.Via {
				via = append(via, g.label(n))
			}
			srcs = append(srcs, strings.Join(via, " > "))
		}
		slices.Sort(srcs)
		got[name] = srcs
	}
	want := map[string][]string{ // the screen it is opened from > the way, from what opens it
		"ViewsActivity":    {"LinkActivity > LinkActivity.onCreate()", "MainActivity > MainActivity.onCreate{λ}"},
		"LinkActivity":     nil, // a deep link, opened by nothing in the app
		"ListFragment":     {"ViewsActivity > @navigation/views_graph > @layout/activity_views > ViewsActivity.<init>()"},
		"DetailFragment":   {"ListFragment > @id/action_list_to_detail > ListFragment.onViewCreated{λ}"},
		"InfoDialog":       {"DetailFragment > @id/infoDialog > DetailFragment.onViewCreated{λ}"},
		"SettingsFragment": {"DetailFragment > @id/settings_graph > DetailFragment.onViewCreated{λ}"},
		"AboutFragment":    {"SettingsFragment > @id/action_settings_to_about > SettingsFragment.onViewCreated{λ}"},
		"LicensesActivity": {"AboutFragment > AboutFragment.onViewCreated{λ}"},
		// the framework calls createIntent on the contract SettingsFragment makes
		"PickActivity": {"SettingsFragment > PickContract.createIntent() > PickContract > SettingsFragment.<init>()"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("opened by\n got %q\nwant %q", got, want)
	}
}
