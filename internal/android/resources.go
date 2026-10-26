package android

// Android resources of a module: what each resource is and what its XML references (resources.py).

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/project"
	"github.com/v-kravchenko/codemap/internal/util"
)

const (
	androidNS = "{http://schemas.android.com/apk/res/android}"
	appNS     = "{http://schemas.android.com/apk/res-auto}"
	toolsNS   = "{http://schemas.android.com/tools}"
)

var fileTypes = map[string]bool{"layout": true, "navigation": true, "drawable": true, "mipmap": true, "menu": true,
	"xml": true, "anim": true, "animator": true, "color": true, "raw": true, "font": true, "transition": true,
	"interpolator": true}

var ValueTags = map[string]string{"string": "string", "color": "color", "dimen": "dimen", "style": "style",
	"plurals": "plurals", "string-array": "array", "integer-array": "array", "array": "array", "bool": "bool",
	"integer": "integer", "fraction": "fraction", "drawable": "drawable"}

var (
	resRef    = regexp.MustCompile(`@(?:\+)?(?:([\w.]+):)?([a-z]+)/([\w.]+)`)
	className = regexp.MustCompile(`^\.?[A-Za-z_][\w]*(\.[A-Za-z_$][\w$]*)+$`)
	lineName  = regexp.MustCompile(`<([\w-]+)\b[^>]*?\bname="([^"]+)"`)
	idOf      = regexp.MustCompile(`^@\+?id/([\w.]+)$`)
)

func ResKey(rtype, name string) string { return rtype + "/" + strings.ReplaceAll(name, ".", "_") }

type Resource struct {
	Key, File string
	Line      int // 0: none
	Refs      map[string]bool
	Classes   map[string]bool
}

func newResource(key, file string, line int) *Resource {
	return &Resource{Key: key, File: file, Line: line, Refs: map[string]bool{}, Classes: map[string]bool{}}
}

// El is an XML element as ElementTree gives it: tag and attribute names `{namespace}local`, the text
// before the first child, comments left out.
type El struct {
	Tag      string
	Attrs    [][2]string
	Text     string
	Tail     string // the text after it, up to the next sibling (valuesSignature)
	Children []*El
}

func (e *El) Get(name string) (string, bool) {
	for _, a := range e.Attrs {
		if a[0] == name {
			return a[1], true
		}
	}
	return "", false
}

func (e *El) Iter(fn func(*El)) {
	fn(e)
	for _, c := range e.Children {
		c.Iter(fn)
	}
}

func xname(n xml.Name) string {
	if n.Space != "" {
		return "{" + n.Space + "}" + n.Local
	}
	return n.Local
}

func ParseXML(data []byte) (*El, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	d := xml.NewDecoder(bytes.NewReader(data))
	var stack []*El
	var root *El
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			el := &El{Tag: xname(t.Name)}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
					continue
				}
				el.Attrs = append(el.Attrs, [2]string{xname(a.Name), a.Value})
			}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.Children = append(p.Children, el)
			} else if root == nil {
				root = el
			}
			stack = append(stack, el)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				if len(p.Children) == 0 {
					p.Text += string(t)
				} else {
					p.Children[len(p.Children)-1].Tail += string(t)
				}
			}
		}
	}
	if root == nil {
		return nil, io.ErrUnexpectedEOF
	}
	return root, nil
}

func ResDirs(moduleDir string) []string {
	var out []string
	for _, sset := range config.SourceSets {
		base := filepath.Join(moduleDir, "src", sset)
		ents, _ := os.ReadDir(base)
		var dirs []string
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), "res") && util.IsDir(filepath.Join(base, e.Name())) {
				dirs = append(dirs, filepath.Join(base, e.Name()))
			}
		}
		sort.Strings(dirs)
		out = append(out, dirs...)
	}
	return out
}

func refsOfText(text string, out map[string]bool) {
	for _, m := range resRef.FindAllStringSubmatch(text, -1) {
		if m[1] == "android" || m[2] == "id" || strings.HasPrefix(m[0], "@+") {
			continue
		}
		out[ResKey(m[2], m[3])] = true
	}
}

var classAttrs = map[string]bool{androidNS + "name": true, "class": true, appNS + "layout_behavior": true}

func scanElement(el *El, r *Resource) {
	if strings.Contains(el.Tag, ".") && !strings.Contains(el.Tag, "}") && className.MatchString(el.Tag) {
		r.Classes[el.Tag] = true
	}
	for _, a := range el.Attrs {
		if strings.HasPrefix(a[0], toolsNS) {
			continue
		}
		if strings.Contains(a[1], "@") {
			refsOfText(a[1], r.Refs)
		}
		if classAttrs[a[0]] && className.MatchString(a[1]) {
			r.Classes[a[1]] = true
		}
	}
	if strings.Contains(el.Text, "@") {
		refsOfText(el.Text, r.Refs)
	}
	for _, c := range el.Children {
		scanElement(c, r)
	}
}

// splitExt is os.path.splitext: leading dots do not start an extension.
func splitExt(base string) string {
	i := strings.LastIndexByte(base, '.')
	if i <= 0 || strings.TrimLeft(base[:i], ".") == "" {
		return base
	}
	return base[:i]
}

func parseFileResource(path, rtype, rel string) *Resource {
	name := splitExt(filepath.Base(path))
	name = strings.TrimSuffix(name, ".9")
	r := newResource(ResKey(rtype, name), rel, 0)
	if strings.HasSuffix(path, ".xml") {
		data, err := os.ReadFile(path)
		if err != nil {
			return r
		}
		root, err := ParseXML(data)
		if err != nil {
			return r
		}
		scanElement(root, r)
	}
	return r
}

func lineIndex(text string) map[[2]string]int {
	out := map[[2]string]int{}
	for i, line := range strings.Split(text, "\n") {
		for _, m := range lineName.FindAllStringSubmatch(line, -1) {
			k := [2]string{m[1], m[2]}
			if _, ok := out[k]; !ok {
				out[k] = i + 1
			}
		}
	}
	return out
}

func parseValues(path, rel string) []*Resource {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	root, err := ParseXML(data)
	if err != nil {
		return nil
	}
	lines := lineIndex(string(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	var out []*Resource
	for _, el := range root.Children {
		name, _ := el.Get("name")
		if name == "" {
			continue
		}
		rtype := ValueTags[el.Tag]
		if el.Tag == "item" {
			rtype, _ = el.Get("type")
		}
		if rtype == "" || rtype == "id" || rtype == "attr" {
			continue
		}
		r := newResource(ResKey(rtype, name), rel, lines[[2]string{el.Tag, name}])
		scanElement(el, r)
		if rtype == "style" {
			parent, has := el.Get("parent")
			if parent != "" && !strings.HasPrefix(parent, "android:") && !strings.HasPrefix(parent, "@android:") {
				r.Refs[ResKey("style", strings.TrimPrefix(parent, "@style/"))] = true
			} else if strings.Contains(name, ".") && !has {
				r.Refs[ResKey("style", name[:strings.LastIndexByte(name, '.')])] = true
			}
		}
		out = append(out, r)
	}
	return out
}

func ResourceTypeOfDir(dir string) string {
	base, _, _ := strings.Cut(dir, "-")
	if base == "values" || fileTypes[base] {
		return base
	}
	return ""
}

func ScanModule(m *project.Module, root string) []*Resource {
	var out []*Resource
	for _, rdir := range m.Res {
		rdir = filepath.Join(root, rdir)
		for _, sub := range util.ListDir(rdir) {
			rtype := ResourceTypeOfDir(sub)
			subPath := filepath.Join(rdir, sub)
			if rtype == "" || !util.IsDir(subPath) {
				continue
			}
			for _, f := range util.ListDir(subPath) {
				path := filepath.Join(subPath, f)
				rel, _ := filepath.Rel(root, path)
				if rtype == "values" {
					if strings.HasSuffix(f, ".xml") {
						out = append(out, parseValues(path, rel)...)
					}
				} else {
					out = append(out, parseFileResource(path, rtype, rel))
				}
			}
		}
	}
	return out
}

type ManifestEntry struct {
	Cls, Kind, Detail string
	Line              int // 0: none
}

func ParseManifest(text string) []ManifestEntry {
	root, err := ParseXML([]byte(text))
	if err != nil {
		return nil
	}
	var out []ManifestEntry
	root.Iter(func(el *El) {
		if el.Tag != "activity" && el.Tag != "activity-alias" {
			return
		}
		name, hasName := el.Get(androidNS + "name")
		cls := name
		if el.Tag == "activity-alias" {
			cls, _ = el.Get(androidNS + "targetActivity")
		}
		if cls == "" {
			return
		}
		if !hasName {
			name = "None"
		}
		line := 0
		if pos := strings.Index(text, `"`+name+`"`); pos >= 0 {
			line = strings.Count(text[:pos], "\n") + 1
		}
		el.Iter(func(f *El) {
			if f.Tag != "intent-filter" {
				return
			}
			var actions []string
			categories := map[string]bool{}
			data := map[string]string{}
			f.Iter(func(x *El) {
				switch x.Tag {
				case "action":
					v, _ := x.Get(androidNS + "name")
					actions = append(actions, v)
				case "category":
					v, _ := x.Get(androidNS + "name")
					categories[v] = true
				case "data":
					for _, a := range []string{"scheme", "host", "path", "pathPrefix", "pathPattern"} {
						if v, _ := x.Get(androidNS + a); v != "" {
							if _, ok := data[a]; !ok {
								data[a] = v
							}
						}
					}
				}
			})
			has := func(s string) bool {
				for _, a := range actions {
					if a == s {
						return true
					}
				}
				return false
			}
			if has("android.intent.action.MAIN") && categories["android.intent.category.LAUNCHER"] {
				out = append(out, ManifestEntry{cls, "launcher", "", line})
			} else if has("android.intent.action.VIEW") && (data["scheme"] != "" || data["host"] != "") {
				path := data["path"]
				if path == "" {
					path = data["pathPrefix"]
				}
				if path == "" {
					path = data["pathPattern"]
				}
				or := func(s string) string {
					if s == "" {
						return "*"
					}
					return s
				}
				out = append(out, ManifestEntry{cls, "deeplink", or(data["scheme"]) + "://" + or(data["host"]) + path, line})
			} else {
				short := make([]string, len(actions))
				for i, a := range actions {
					short[i] = a[strings.LastIndexByte(a, '.')+1:]
				}
				out = append(out, ManifestEntry{cls, "intent", strings.Join(short, ", "), line})
			}
		})
	})
	return out
}

func navID(v string, ok bool) string {
	if !ok {
		return ""
	}
	if m := idOf.FindStringSubmatch(v); m != nil {
		return m[1]
	}
	return ""
}

// NavGraph: destinations {id: class}, nested graphs {id: start id} ("" key: the root graph's start),
// actions [(id, destination id)].
type NavGraph struct {
	Dests    map[string]string
	DestIDs  []string // Dests keys in document order
	Graphs   map[string]string
	GraphIDs []string // Graphs keys in document order ("" included)
	Actions  [][2]string
}

func ParseNavigation(text []byte) *NavGraph {
	g := &NavGraph{Dests: map[string]string{}, Graphs: map[string]string{}}
	root, err := ParseXML(text)
	if err != nil {
		return g
	}
	setGraph := func(k, v string) {
		if _, ok := g.Graphs[k]; !ok {
			g.GraphIDs = append(g.GraphIDs, k)
		}
		g.Graphs[k] = v
	}
	root.Iter(func(el *El) {
		switch el.Tag {
		case "navigation":
			gid := navID(el.Get(androidNS + "id"))
			start := navID(el.Get(appNS + "startDestination"))
			if gid != "" && start != "" {
				setGraph(gid, start)
			}
			if el == root && start != "" {
				setGraph("", start)
			}
		case "action":
			aid, dest := navID(el.Get(androidNS+"id")), navID(el.Get(appNS+"destination"))
			if aid != "" && dest != "" {
				g.Actions = append(g.Actions, [2]string{aid, dest})
			}
		default:
			did := navID(el.Get(androidNS + "id"))
			cls, _ := el.Get(androidNS + "name")
			if did != "" && cls != "" && className.MatchString(cls) && el.Tag != "argument" {
				if _, ok := g.Dests[did]; !ok {
					g.DestIDs = append(g.DestIDs, did)
				}
				g.Dests[did] = cls
			}
		}
	})
	return g
}

// write: the element with its attributes, text, children and their tails (ET.tostring, for equality).
func (e *El) Write(b *strings.Builder) {
	fmt.Fprintf(b, "<%q", e.Tag)
	for _, a := range e.Attrs {
		fmt.Fprintf(b, " %q=%q", a[0], a[1])
	}
	fmt.Fprintf(b, ">%q", e.Text)
	for _, c := range e.Children {
		c.Write(b)
		fmt.Fprintf(b, "%q", c.Tail)
	}
	b.WriteString("</>")
}
