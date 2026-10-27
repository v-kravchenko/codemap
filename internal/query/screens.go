package query

// screens and build --info.

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/util"
)

func (g *Graph) cmdScreens(o opts) int {
	filter := ""
	if len(o.pos) > 0 {
		filter = strings.ToLower(o.pos[0])
	}
	type row struct {
		kind, module, label, target, host, loc string
		hosts                                  []int32
		content                                []string
		ends                                   []navRoot
	}
	var out []row
	hidden, shown := map[string]int{}, shownKinds(o)
	mf, outside := g.moduleFilter(o.modules), 0
	reached := map[int32]bool{} // by the walks of --unreachable
	var hostIn map[int32]bool
	if o.host != "" {
		hostIn = map[int32]bool{}
		for _, h := range g.screensOf(o.host, o.all) {
			hostIn[h] = true
		}
	}
	s := &g.m.Screen
	for i := range s.Node {
		n := g.m.Index(s.Node[i])
		kind, label, target := s.Kind.At(i), s.Label.At(i), s.Target.At(i)
		module, hosts := g.module(n), g.hosts(n)
		var hl []string
		for _, h := range hosts {
			hl = append(hl, g.label(h))
		}
		host := strings.Join(hl, ", ")
		var content []string
		for _, c := range g.drawn[n] {
			content = append(content, g.label(c))
		}
		slices.Sort(content)
		if filter != "" && !slices.ContainsFunc(append([]string{label, target, module, host}, content...),
			func(x string) bool { return strings.Contains(strings.ToLower(x), filter) }) {
			continue
		}
		if !mf.keeps(module) {
			outside++
			continue
		}
		if hostIn != nil && !slices.ContainsFunc(hosts, func(h int32) bool { return hostIn[h] }) {
			continue
		}
		if !shown[kind] {
			hidden[kind]++
			continue
		}
		var ends []navRoot
		if o.unreachable {
			if !screenKinds[kind] {
				continue // previews and layouts are not opened, so not reached either
			}
			var dead bool
			if ends, dead = g.unreached(n, reached); !dead {
				continue
			}
		}
		out = append(out, row{kind, module, label, target, host, g.location(n), hosts, content, ends})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		for _, p := range [][2]string{{a.kind, b.kind}, {a.module, b.module}, {a.label, b.label}, {a.target, b.target},
			{a.host, b.host}, {a.loc, b.loc}} {
			if p[0] != p[1] {
				return p[0] < p[1]
			}
		}
		return slices.Compare(a.hosts, b.hosts) < 0
	})
	if o.json {
		items := make([]any, len(out))
		for i, r := range out {
			item := append(append(obj{{"kind", r.kind}, {"module", r.module}, {"name", r.label}, {"target", r.target}},
				g.hostsJSON(r.hosts)...), kv{"location", r.loc}, kv{"content", strs(r.content)})
			if o.unreachable {
				var ends []any
				for _, e := range r.ends {
					ends = append(ends, g.entryJSON(e.sid, e.how))
				}
				item = append(item, kv{"entries", list(ends)})
			}
			items[i] = item
		}
		result := obj{{"screens", items}, {"hidden", countsObj(hidden)}}
		if mf != nil {
			result = append(result, kv{"hidden_by_module", obj{{"screens", outside}}})
		}
		g.printJSON("screens", result)
		return exitOK
	}
	for _, t := range config.ScreenTitles {
		var items []row
		for _, r := range out {
			if r.kind == t[0] {
				items = append(items, r)
			}
		}
		if len(items) == 0 {
			continue
		}
		fmt.Printf("\n%s (%d)\n", t[1], len(items))
		for _, r := range items {
			name := r.label
			if r.kind == "preview" {
				name = r.target
			}
			host := ""
			if r.host != "" {
				host = "  host " + r.host
			}
			fmt.Printf("  %-50s %-28s %s%s\n", name, r.module, r.loc, host)
			if len(r.content) > 0 {
				fmt.Printf("      content %s\n", strings.Join(r.content, ", "))
			}
			for _, e := range r.ends {
				fmt.Printf("      ends at %s: %s\n", g.screenName(e.sid), deadEnd(e.how[0]))
			}
		}
	}
	if len(hidden) > 0 {
		fmt.Println("\n" + hiddenLine(hidden))
	}
	if outside > 0 {
		fmt.Println("\n" + outsideLine(fmt.Sprintf("%d screens", outside)))
	}
	if o.unreachable {
		fmt.Printf("\n(%d screens no entry of the manifest reaches, as far as the code map sees)\n", len(out))
	}
	return exitOK
}

// deadEnd: how a way back from an unreachable screen ends, for reading.
func deadEnd(e navEntry) string {
	switch e.kind {
	case "unknown":
		return "nothing opens it"
	case "outside":
		return "opened only from code outside the screens"
	}
	return entryText(e)
}

func (g *Graph) cmdBuildInfo(o opts) int {
	screens := map[string]int{}
	for i := range g.m.Screen.Node {
		screens[g.m.Screen.Kind.At(i)]++
	}
	meta := map[string]string{}
	for i := range g.m.Meta.Key.Len() {
		meta[g.m.Meta.Key.At(i)] = g.m.Meta.Value.At(i)
	}
	if o.json {
		index := obj{}
		for _, k := range util.SortedKeys(meta) {
			index = append(index, kv{k, meta[k]})
		}
		g.printJSON("build", obj{{"index", index}, {"screens", countsObj(screens)}})
		return exitOK
	}
	for _, k := range util.SortedKeys(meta) {
		v := meta[k]
		if k == "class_dirs" {
			var dirs []string
			json.Unmarshal([]byte(v), &dirs)
			v = fmt.Sprintf("%d directories", len(dirs))
		}
		if k == "config" || k == "module_paths" {
			continue // long JSON: `codemap config print`, codemap.model.json
		}
		fmt.Printf("%-14s %s\n", k, v)
	}
	for _, k := range util.SortedKeys(screens) {
		fmt.Printf("screens.%-6s %d\n", k, screens[k])
	}
	return exitOK
}

func hiddenLine(hidden map[string]int) string {
	var parts []string
	for _, t := range config.ScreenTitles {
		if hidden[t[0]] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", t[0], hidden[t[0]]))
		}
	}
	return "Not shown: " + strings.Join(parts, ", ") + " (--kind KIND, --kind all)"
}
