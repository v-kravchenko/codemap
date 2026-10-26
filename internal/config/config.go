package config

import (
	"bytes"
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/v-kravchenko/codemap/internal/util"
	"go.yaml.in/yaml/v3"
)

// configFile: the project's settings, at the root next to settings.gradle. Without it the tool runs
// on defaultExtends.
const ConfigFile = "codemap.yml"

var defaultExtends = []string{"android", "nav2-compose"}

//go:embed presets/*.yml
var presetFS embed.FS

//go:embed codemap.schema.json
var configSchema []byte

// Config: codemap.yml after its presets. Lists add up from the presets to the file (orderedLists
// replace); a scalar set later wins. Class names are dotted (com.example.Base, Outer$Inner); member patterns are
// Class.member globs, see memberPattern.
type Config struct {
	Version  int      `yaml:"version" json:"version"`
	Extends  []string `yaml:"extends,omitempty" json:"extends,omitempty"`
	Variant  string   `yaml:"variant,omitempty" json:"variant,omitempty"`
	Snapshot string   `yaml:"snapshot,omitempty" json:"snapshot,omitempty"`
	Build    struct {
		ClassDirs  []string `yaml:"class_dirs,omitempty" json:"class_dirs,omitempty"`
		SourceSets []string `yaml:"source_sets,omitempty" json:"source_sets,omitempty"`
	} `yaml:"build,omitempty" json:"build"`
	Changes struct {
		Base []string `yaml:"base,omitempty" json:"base,omitempty"`
	} `yaml:"changes,omitempty" json:"changes"`
	Screens struct {
		ActivityBases      []string `yaml:"activity_bases,omitempty" json:"activity_bases,omitempty"`
		FragmentBases      []string `yaml:"fragment_bases,omitempty" json:"fragment_bases,omitempty"`
		DialogBases        []string `yaml:"dialog_bases,omitempty" json:"dialog_bases,omitempty"`
		PreviewAnnotations []string `yaml:"preview_annotations,omitempty" json:"preview_annotations,omitempty"`
		PreviewNote        string   `yaml:"preview_note,omitempty" json:"preview_note,omitempty"`
		LayoutPattern      string   `yaml:"layout_pattern,omitempty" json:"layout_pattern,omitempty"`
		DebugSourceSets    []string `yaml:"debug_source_sets,omitempty" json:"debug_source_sets,omitempty"`
	} `yaml:"screens,omitempty" json:"screens"`
	Navigation struct {
		RouteBases       []string `yaml:"route_bases,omitempty" json:"route_bases,omitempty"`
		ChildBases       []string `yaml:"child_bases,omitempty" json:"child_bases,omitempty"`
		RouteInterfaces  []string `yaml:"route_interfaces,omitempty" json:"route_interfaces,omitempty"`
		Register         []string `yaml:"register,omitempty" json:"register,omitempty"`
		Open             []string `yaml:"open,omitempty" json:"open,omitempty"`
		Graph            []string `yaml:"graph,omitempty" json:"graph,omitempty"`
		NotOpening       []string `yaml:"not_opening,omitempty" json:"not_opening,omitempty"`
		StartDestination string   `yaml:"start_destination,omitempty" json:"start_destination,omitempty"`
		TypedRoutes      bool     `yaml:"typed_routes,omitempty" json:"typed_routes,omitempty"`
		StringRoutes     bool     `yaml:"string_routes,omitempty" json:"string_routes,omitempty"`
		HintSkip         []string `yaml:"hint_skip,omitempty" json:"hint_skip,omitempty"`
	} `yaml:"navigation,omitempty" json:"navigation"`
	NavPath struct {
		PushMarkers []string `yaml:"push_markers,omitempty" json:"push_markers,omitempty"`
		Costs       struct {
			Own    int `yaml:"own,omitempty" json:"own,omitempty"`
			Shared int `yaml:"shared,omitempty" json:"shared,omitempty"`
			Push   int `yaml:"push,omitempty" json:"push,omitempty"`
		} `yaml:"costs,omitempty" json:"costs"`
		BranchSlack int `yaml:"branch_slack,omitempty" json:"branch_slack,omitempty"`
		PathLimit   int `yaml:"path_limit,omitempty" json:"path_limit,omitempty"`
		HubMin      int `yaml:"hub_min,omitempty" json:"hub_min,omitempty"`
	} `yaml:"nav_path,omitempty" json:"nav_path"`
	Graph struct {
		Fanout   int `yaml:"fanout,omitempty" json:"fanout,omitempty"`
		MaxEdges int `yaml:"max_edges,omitempty" json:"max_edges,omitempty"`
	} `yaml:"graph,omitempty" json:"graph"`
}

func presetNames() []string {
	ents, _ := presetFS.ReadDir("presets")
	var out []string
	for _, e := range ents {
		out = append(out, strings.TrimSuffix(e.Name(), ".yml"))
	}
	return out
}

// decodeConfig: one YAML document, unknown keys rejected.
func decodeConfig(name string, data []byte) (*Config, error) {
	var c Config
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %v", name, err)
	}
	return &c, nil
}

func preset(name string, seen []string) (*Config, error) {
	if slices.Contains(seen, name) {
		return nil, fmt.Errorf("preset %s extends itself: %s", name, strings.Join(append(seen, name), " -> "))
	}
	data, err := presetFS.ReadFile("presets/" + name + ".yml")
	if err != nil {
		return nil, fmt.Errorf("unknown preset %q (presets: %s)", name, strings.Join(presetNames(), ", "))
	}
	c, err := decodeConfig("preset "+name, data)
	if err != nil {
		return nil, err
	}
	return resolve(c, append(seen, name))
}

// resolve: the presets c extends merged first, then c over them.
func resolve(c *Config, seen []string) (*Config, error) {
	out := &Config{}
	for _, name := range c.Extends {
		p, err := preset(name, seen)
		if err != nil {
			return nil, err
		}
		merge(out, p)
	}
	merge(out, c)
	return out, nil
}

// orderedLists: lists tried in order; a later one replaces the earlier instead of adding to it.
var orderedLists = util.Set("build.class_dirs", "build.source_sets", "changes.base")

// merge: src over dst, field by field. Lists are added without repeats (orderedLists replace);
// non-zero scalars replace.
func merge(dst, src *Config) {
	var walk func(path string, d, s map[string]any)
	walk = func(path string, d, s map[string]any) {
		for k, sv := range s {
			switch v := sv.(type) {
			case map[string]any:
				dv, _ := d[k].(map[string]any)
				if dv == nil {
					dv = map[string]any{}
				}
				walk(path+k+".", dv, v)
				d[k] = dv
			case []any:
				if orderedLists[path+k] {
					d[k] = v
					continue
				}
				dv, _ := d[k].([]any)
				for _, x := range v {
					if !slices.Contains(dv, x) {
						dv = append(dv, x)
					}
				}
				d[k] = dv
			default:
				d[k] = sv
			}
		}
	}
	dm, sm := toMap(dst), toMap(src)
	walk("", dm, sm)
	data, _ := json.Marshal(dm)
	*dst = Config{}
	json.Unmarshal(data, dst)
}

func toMap(c *Config) map[string]any {
	data, _ := json.Marshal(c)
	m := map[string]any{}
	json.Unmarshal(data, &m)
	return m
}

// loadConfig: ROOT/codemap.yml with its presets, checked; defaultExtends when there is no file.
func LoadConfig(root string) (*Config, error) {
	path := filepath.Join(root, ConfigFile)
	data, err := os.ReadFile(path)
	var c *Config
	if os.IsNotExist(err) {
		c = &Config{Version: 1, Extends: defaultExtends}
	} else if err != nil {
		return nil, err
	} else if c, err = decodeConfig(ConfigFile, data); err != nil {
		return nil, err
	}
	if c.Version != 1 {
		return nil, fmt.Errorf("%s: version must be 1, got %d", ConfigFile, c.Version)
	}
	r, err := resolve(c, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", ConfigFile, err)
	}
	r.Version, r.Extends = 1, c.Extends
	if _, err := r.compile(); err != nil {
		return nil, fmt.Errorf("%s: %v", ConfigFile, err)
	}
	return r, nil
}

func (c *Config) JSON() string {
	data, _ := json.Marshal(c)
	return string(data)
}

func (c *Config) Sha1() string {
	s := sha1.Sum([]byte(c.JSON()))
	return hex.EncodeToString(s[:])
}

// snapshotPath: where the map is, from the config's snapshot (relative to root); the queries read
// only this key of the file, the rest comes with the map.
func snapshotPath(root string) string {
	rel := "build/codemap/codemap.snap"
	if data, err := os.ReadFile(filepath.Join(root, ConfigFile)); err == nil {
		var c struct {
			Snapshot string `yaml:"snapshot"`
		}
		if yaml.Unmarshal(data, &c) == nil && c.Snapshot != "" {
			rel = c.Snapshot
		}
	}
	return filepath.Join(root, rel)
}

// -- compiled settings, what the builder and the queries use

type compiled struct {
	activityBases, fragmentBases, dialogBases map[string]bool
	previewMarkers                            map[string]bool
	screenLayout                              *regexp.Regexp
	routeRegistration, routeByName            *regexp.Regexp
	routeGraph                                *regexp.Regexp
	routeBases, childBases, routeInterfaces   map[string]bool
	routeReadOnly                             *regexp.Regexp
}

func (c *Config) compile() (*compiled, error) {
	var errs []string
	members := func(key string, pats []string) *regexp.Regexp {
		var alts []string
		for _, p := range pats {
			if !strings.Contains(p, ".") {
				errs = append(errs, fmt.Sprintf("%s: %q is not Class.member", key, p))
				continue
			}
			alts = append(alts, memberPattern(p))
		}
		return alternatives(alts)
	}
	names := func(pats []string) *regexp.Regexp {
		var alts []string
		for _, p := range pats {
			alts = append(alts, globPattern(p))
		}
		return alternatives(alts)
	}
	k := &compiled{
		activityBases:     classSet(c.Screens.ActivityBases),
		fragmentBases:     classSet(slices.Concat(c.Screens.FragmentBases, c.Screens.DialogBases)),
		dialogBases:       classSet(c.Screens.DialogBases),
		previewMarkers:    util.Set(c.Screens.PreviewAnnotations...),
		routeRegistration: members("navigation.register", c.Navigation.Register),
		routeByName:       members("navigation.open", c.Navigation.Open),
		routeGraph:        members("navigation.graph", c.Navigation.Graph),
		routeBases:        classSet(c.Navigation.RouteBases),
		childBases:        classSet(c.Navigation.ChildBases),
		routeInterfaces:   classSet(c.Navigation.RouteInterfaces),
		routeReadOnly:     names(append([]string{"<init>"}, c.Navigation.NotOpening...)),
	}
	k.screenLayout = regexp.MustCompile(`$^`)
	if p := c.Screens.LayoutPattern; p != "" {
		re, err := regexp.Compile(p)
		if err != nil {
			errs = append(errs, fmt.Sprintf("screens.layout_pattern: %v", err))
		} else {
			k.screenLayout = re
		}
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	return k, nil
}

func classSet(names []string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[strings.ReplaceAll(n, ".", "/")] = true
	}
	return m
}

// globPattern: a name glob as a regexp: * stands for a run of word characters.
func globPattern(p string) string {
	parts := strings.Split(p, "*")
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	return strings.Join(parts, `\w*`)
}

// memberPattern: com.example.*.open* -> com/example/\w*\.open\w*: the last dot parts class from
// member, the dots before it are the package's.
func memberPattern(p string) string {
	i := strings.LastIndex(p, ".")
	return globPattern(strings.ReplaceAll(p[:i], ".", "/")) + `\.` + globPattern(p[i+1:])
}

// alternatives: ^(a|b)($default)?$ (a Kotlin function with default arguments compiles to name$default);
// nil patterns match nothing.
func alternatives(alts []string) *regexp.Regexp {
	if len(alts) == 0 {
		return regexp.MustCompile(`$^`)
	}
	return regexp.MustCompile(`^(` + strings.Join(alts, "|") + `)(\$default)?$`)
}

// applyConfig: c into the settings the builder and the queries read.
func ApplyConfig(c *Config) {
	k, err := c.compile()
	if err != nil {
		panic(util.UserError(err.Error()))
	}
	Cfg = c
	ActivityBases, FragmentBases, DialogBases = k.activityBases, k.fragmentBases, k.dialogBases
	PreviewMarkers, ScreenLayout = k.previewMarkers, k.screenLayout
	RouteRegistration, RouteByName, RouteReadOnly = k.routeRegistration, k.routeByName, k.routeReadOnly
	RouteGraph = k.routeGraph
	RouteBases, ChildBases, RouteInterfaces = k.routeBases, k.childBases, k.routeInterfaces
	StartDestination = c.Navigation.StartDestination
	TypedRoutes, StringRoutes = c.Navigation.TypedRoutes, c.Navigation.StringRoutes
	for i, t := range ScreenTitles {
		if t[0] == "preview" {
			ScreenTitles[i][1] = "Screen previews"
			if c.Screens.PreviewNote != "" {
				ScreenTitles[i][1] += " (" + c.Screens.PreviewNote + ")"
			}
		}
	}
	variant := c.Variant
	if variant == "" {
		variant = "debug"
	}
	expand := func(xs []string) []string {
		var out []string
		for _, x := range xs {
			x = strings.ReplaceAll(x, "{variant}", variant)
			out = append(out, strings.ReplaceAll(x, "{Variant}", strings.ToUpper(variant[:1])+variant[1:]))
		}
		return out
	}
	ClassDirs, SourceSets = expand(c.Build.ClassDirs), expand(c.Build.SourceSets)
	manifests = nil
	for _, s := range SourceSets {
		manifests = append(manifests, "src/"+s+"/AndroidManifest.xml")
	}
	pick := func(v, def int) int {
		if v != 0 {
			return v
		}
		return def
	}
	n := c.NavPath
	StepOwn, StepShared, StepPush = pick(n.Costs.Own, 1), pick(n.Costs.Shared, 3), pick(n.Costs.Push, 30)
	BranchSlack, PathLimit, HubMin = pick(n.BranchSlack, 4), pick(n.PathLimit, 64), pick(n.HubMin, 10)
	GraphFanout, GraphEdges = pick(c.Graph.Fanout, 15), pick(c.Graph.MaxEdges, 400)
}

// configCmd: codemap config check|print|schema.
func ConfigCmd(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: codemap config check|print|schema")
		return 2
	}
	root := util.FindRoot()
	switch args[0] {
	case "schema":
		os.Stdout.Write(configSchema)
		return 0
	case "check", "print":
	default:
		fmt.Fprintln(os.Stderr, "usage: codemap config check|print|schema")
		return 2
	}
	c, err := LoadConfig(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		return 1
	}
	if args[0] == "print" {
		data, _ := yaml.Marshal(c)
		os.Stdout.Write(data)
		return 0
	}
	src := ConfigFile
	if !util.IsFile(filepath.Join(root, ConfigFile)) {
		src = "no " + ConfigFile + ", defaults"
	}
	fmt.Printf("ok: %s, presets %s, variant %s\n", src, strings.Join(c.Extends, ", "), c.Variant)
	return 0
}

// Set by applyConfig from codemap.yml.
var (
	Cfg                                       *Config
	ClassDirs, SourceSets, manifests          []string
	ActivityBases, DialogBases, FragmentBases map[string]bool
	PreviewMarkers                            map[string]bool
	ScreenLayout                              *regexp.Regexp
	RouteRegistration, RouteByName            *regexp.Regexp
	RouteGraph                                *regexp.Regexp
	RouteBases, ChildBases, RouteInterfaces   map[string]bool
	RouteReadOnly                             *regexp.Regexp
	StartDestination                          string
	TypedRoutes, StringRoutes                 bool
)

// Set by applyConfig. nav-path step costs: a screen opens the next one through code of its own,
// through shared code (a navigator many screens call), or through push or deep link handling (only
// for a notification or a link); branchSlack: the other branches of the launcher shown, up to this
// much dearer.
var (
	StepOwn, StepShared, StepPush int
	BranchSlack                   int
	GraphFanout, GraphEdges       int
	PathLimit                     int // the shortest ways opened_by tries for one pair
	HubMin                        int // screens below a node that make it a hub
)

// screenTitles: the screen kinds in the order of the output, with their headings (a preview note set by applyConfig).
var ScreenTitles = [][2]string{{"activity", "Activities"}, {"fragment", "Fragments"}, {"dialog", "Dialog fragments"}, {"route", "Compose routes"}, {"preview", "Screen previews"}, {"layout", "Screen layouts"}}

func DefaultSnapshot(root string) string { return snapshotPath(root) }
