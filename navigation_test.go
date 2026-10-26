package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// fixtureRoot: testdata/navapp with its classes (classes.zip, made by update.sh) under app/classes.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	src := filepath.Join("testdata", "navapp")
	root := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		rel, _ := filepath.Rel(src, path)
		if err != nil || d.IsDir() || !(rel == "settings.gradle.kts" || rel == "codemap.yml" ||
			strings.HasPrefix(rel, filepath.Join("app", "src")+string(filepath.Separator))) {
			return err
		}
		data, err := os.ReadFile(path)
		if err == nil {
			os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755)
			err = os.WriteFile(filepath.Join(root, rel), data, 0o644)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(filepath.Join(src, "classes.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		path := filepath.Join(root, "app", "classes", f.Name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(r)
		r.Close()
		os.WriteFile(path, data, 0o644)
	}
	return root
}

// TestNavigationPresets: the routes of nav2-compose, nav2-compose-strings and nav3 in the fixture,
// and the methods that open each.
func TestNavigationPresets(t *testing.T) {
	root := fixtureRoot(t)
	c, err := loadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	applyConfig(c)
	t.Cleanup(func() {
		d, _ := loadConfig(t.TempDir())
		applyConfig(d)
	})
	b := NewBuilder(root, discoverModules(root), 2, func(string) {})
	b.Build()

	opened := map[string][]string{}
	for e := range b.edges {
		if e.Kind != OPENS || b.nodes[e.Dst].Kind != "ROUTE" {
			continue
		}
		m := b.nodes[e.Src]
		name := strings.TrimPrefix(b.nodes[m.Owner].Name, "navapp/") + "." + m.Name
		opened[b.nodes[e.Dst].Sname] = append(opened[b.nodes[e.Dst].Sname], name)
	}
	got := map[string][]string{}
	for _, s := range b.screens {
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
		"Settings": {"typed/SettingsViewModel$openSettings$1.invokeSuspend", "typed/TypedAppKt.TypedApp$lambda$0$0$1$0$0"},
		"Confirm":  {"typed/TypedAppKt.TypedApp$lambda$0$0$2"},
		"Login":    {"typed/TypedAppKt.TypedApp$lambda$0$0$0$1$0"},
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

// TestViewsNavigation: who opens the fragments of an XML navigation graph (its start through the
// host's layout, an <action>, a nested graph, a dialog) and an Activity started with an Intent.
func TestViewsNavigation(t *testing.T) {
	root := fixtureRoot(t)
	c, err := loadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	applyConfig(c)
	t.Cleanup(func() {
		d, _ := loadConfig(t.TempDir())
		applyConfig(d)
	})
	b := NewBuilder(root, discoverModules(root), 2, func(string) {})
	b.Build()
	m := b.Map(map[string]string{"root": root})
	m.precomputeOpenedBy(root, 2)
	g := NewGraph(m, root)

	got := map[string][]string{}
	for sid := range g.screens {
		name := g.screenName(sid)
		if !strings.HasSuffix(name, "Fragment") && !strings.HasSuffix(name, "Activity") && name != "InfoDialog" ||
			name == "MainActivity" {
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
		"ViewsActivity":    {"MainActivity > MainActivity.onCreate{λ}"},
		"ListFragment":     {"ViewsActivity > @navigation/views_graph > @layout/activity_views > ViewsActivity.<init>()"},
		"DetailFragment":   {"ListFragment > @id/action_list_to_detail > ListFragment.onViewCreated{λ}"},
		"InfoDialog":       {"DetailFragment > @id/infoDialog > DetailFragment.onViewCreated{λ}"},
		"SettingsFragment": {"DetailFragment > @id/settings_graph > DetailFragment.onViewCreated{λ}"},
		"AboutFragment":    {"SettingsFragment > @id/action_settings_to_about > SettingsFragment.onViewCreated{λ}"},
		"LicensesActivity": {"AboutFragment > AboutFragment.onViewCreated{λ}"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("opened by\n got %q\nwant %q", got, want)
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
