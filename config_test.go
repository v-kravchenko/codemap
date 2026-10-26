package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, text string) string {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, configFile), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestConfigMerge(t *testing.T) {
	c, err := loadConfig(writeConfig(t, `
version: 1
extends: [android]
variant: googleDebug
changes: {base: [origin/develop]}
screens:
  fragment_bases: [com.example.BaseFragment]
nav_path: {costs: {push: 50}}
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Variant != "googleDebug" || c.NavPath.Costs.Push != 50 || c.NavPath.Costs.Own != 1 {
		t.Errorf("scalars: variant %s, costs %+v", c.Variant, c.NavPath.Costs)
	}
	if !slices.Equal(c.Changes.Base, []string{"origin/develop"}) {
		t.Errorf("changes.base must replace the preset's: %v", c.Changes.Base)
	}
	if !slices.Equal(c.Screens.FragmentBases, []string{"androidx.fragment.app.Fragment", "com.example.BaseFragment"}) {
		t.Errorf("fragment_bases must add up: %v", c.Screens.FragmentBases)
	}
}

func TestConfigErrors(t *testing.T) {
	for text, want := range map[string]string{
		"version: 1\nscreen: {}\n":                      "field screen not found",
		"version: 2\n":                                  "version must be 1",
		"version: 1\nextends: [koin]\n":                 `unknown preset "koin"`,
		"version: 1\nnavigation: {open: [navigate]}\n":  "is not Class.member",
		"version: 1\nscreens: {layout_pattern: '(x'}\n": "screens.layout_pattern",
	} {
		_, err := loadConfig(writeConfig(t, text))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", text, err, want)
		}
	}
}

func TestConfigDefaults(t *testing.T) {
	c, err := loadConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Extends, defaultExtends) || c.Variant != "debug" {
		t.Errorf("defaults: %+v", c)
	}
}

func TestMemberPattern(t *testing.T) {
	re := alternatives([]string{memberPattern("com.example.nav.*.screen*"), memberPattern("com.example.Nav$To.<init>")})
	for s, want := range map[string]bool{
		"com/example/nav/NavKt.screen":             true,
		"com/example/nav/NavKt.screenArgs$default": true,
		"com/example/nav/sub/NavKt.screen":         false,
		"com/example/Nav$To.<init>":                true,
		"com/example/NavXTo.<init>":                false,
	} {
		if re.MatchString(s) != want {
			t.Errorf("%s: want %v", s, want)
		}
	}
}

// TestSchemaCoversConfig: every key of Config is in codemap.schema.json and the other way round.
func TestSchemaCoversConfig(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(configSchema, &schema); err != nil {
		t.Fatal(err)
	}
	var fromSchema, fromType []string
	var walkSchema func(prefix string, s map[string]any)
	walkSchema = func(prefix string, s map[string]any) {
		props, _ := s["properties"].(map[string]any)
		for k, v := range props {
			fromSchema = append(fromSchema, prefix+k)
			walkSchema(prefix+k+".", v.(map[string]any))
		}
	}
	walkSchema("", schema)
	var walkType func(prefix string, t reflect.Type)
	walkType = func(prefix string, t reflect.Type) {
		for i := range t.NumField() {
			f := t.Field(i)
			k, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
			fromType = append(fromType, prefix+k)
			if f.Type.Kind() == reflect.Struct {
				walkType(prefix+k+".", f.Type)
			}
		}
	}
	walkType("", reflect.TypeOf(Config{}))
	slices.Sort(fromSchema)
	slices.Sort(fromType)
	if !slices.Equal(fromSchema, fromType) {
		t.Errorf("schema keys %v\nConfig keys %v", fromSchema, fromType)
	}
	presets := schema["properties"].(map[string]any)["extends"].(map[string]any)["items"].(map[string]any)["enum"]
	var names []string
	for _, p := range presets.([]any) {
		names = append(names, p.(string))
	}
	if !slices.Equal(names, presetNames()) {
		t.Errorf("schema presets %v, embedded %v", names, presetNames())
	}
}

func TestPresetsLoad(t *testing.T) {
	for _, name := range presetNames() {
		c, err := preset(name, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.compile(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
