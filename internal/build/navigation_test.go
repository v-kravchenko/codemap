package build

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/fixture"
	"github.com/v-kravchenko/codemap/internal/snapshot"
)

// TestNavigationPresets: the routes of nav2-compose, nav2-compose-strings and nav3 in the fixture,
// and the methods that open each.
func TestNavigationPresets(t *testing.T) {
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
	b := NewBuilder(root, DiscoverModules(root), 2, func(string) {})
	b.Build()

	opened := map[string][]string{}
	for e := range b.Edges {
		if e.Kind != snapshot.OPENS || b.Nodes[e.Dst].Kind != "ROUTE" {
			continue
		}
		m := b.Nodes[e.Src]
		name := strings.TrimPrefix(b.Nodes[m.Owner].Name, "navapp/") + "." + m.Name
		opened[b.Nodes[e.Dst].Sname] = append(opened[b.Nodes[e.Dst].Sname], name)
	}
	got := map[string][]string{}
	for _, s := range b.Screens {
		if s.Kind == "route" {
			slices.Sort(opened[s.Label])
			got[s.Label] = opened[s.Label]
		}
	}
	want := map[string][]string{
		// nav2-compose: NavHost's start, navigate(Route) and navigate(Route(args)) in clicks, a Flow;
		// opening the nested graph AuthGraph opens its start Login
		"Home":     {"typed/TypedAppKt.TypedApp"},
		"Profile":  {"typed/TypedAppKt.TypedApp$lambda$0$0$0$0$0"},
		"Settings": {"typed/SettingsViewModel$openSettings$1.invokeSuspend", "typed/TypedAppKt.TypedApp$lambda$0$0$0$1$0", "typed/TypedAppKt.TypedApp$lambda$0$0$1$0$0"},
		"Confirm":  {"typed/TypedAppKt.TypedApp$lambda$0$0$2"},
		"Login":    {"typed/TypedAppKt.TypedApp$lambda$0$0$0$2$0"},
		// nav2-compose-strings: {id} and $id match any segment, the query is left out; "account" is a
		// nested graph that opens signin
		"feed":              {"strings/StringAppKt.StringApp", "strings/StringAppKt.StringApp$lambda$0$0$3$0$0$0"},
		"post/{id}":         {"strings/StringAppKt.StringApp$lambda$0$0$0$0$0"},
		"comments/{postId}": {"strings/StringAppKt.StringApp$lambda$0$0$2$0$0"},
		"signin":            {"strings/StringAppKt.StringApp$lambda$0$0$0$1$0"},
		// nav3: entry<T>, rememberNavBackStack(start), backStack.add(...)
		"Inbox":   {"nav3dsl/Nav3DslAppKt.Nav3DslApp"},
		"Message": {"nav3dsl/Nav3DslAppKt.Nav3DslApp$lambda$1$0$0$0"},
		"Compose": {"nav3dsl/Nav3DslAppKt.Nav3DslApp$lambda$1$1$0$0"},
		// nav3: `is T -> NavEntry(key)` in an entryProvider lambda
		"Catalog": {"nav3when/Nav3WhenAppKt.Nav3WhenApp"},
		"Item":    {"nav3when/Nav3WhenAppKt.Nav3WhenApp$lambda$2$0$0$0$0"},
		// nav2-compose through the project's own inline wrappers: section<T>(start) opens its start
		"Welcome": {"sections/SectionsAppKt.SectionsApp"},
		"Terms":   {"sections/SectionsAppKt$SectionsApp$1$1$1$1.invoke"},
		"About":   {"sections/SectionsAppKt$SectionsApp$1$1$1$2.invoke"},
	}
	if !reflect.DeepEqual(got, want) {
		for k, v := range want {
			if !reflect.DeepEqual(got[k], v) {
				t.Errorf("%s opened by %q, want %q", k, got[k], v)
			}
		}
		for k, v := range got {
			if _, ok := want[k]; !ok {
				t.Errorf("unexpected route %s opened by %q", k, v)
			}
		}
	}
}

func TestRouteSegments(t *testing.T) {
	for _, c := range []struct {
		open, route string
		match       bool
	}{
		{"post/42", "post/{id}", true},
		{"comments/\x01?sort=new", "comments/{postId}", true},
		{"post", "post/{id}", false},
		{"feed", "feed", true},
		{"feed/x", "feed", false},
		{"\x01", "feed", true},
	} {
		if got := segmentsMatch(routeSegments(c.open), routeSegments(c.route)); got != c.match {
			t.Errorf("%q against %q: %v, want %v", c.open, c.route, got, c.match)
		}
	}
}
