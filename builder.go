package main

// Builds the code map from compiled debug classes and module resources (builder.py).

import (
	"bytes"
	"cmp"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/blake2b"
)

const (
	CALL = iota + 1
	READ
	WRITE
	TYPE
	EXTENDS
	IMPLEMENTS
	LAMBDA
	INLINE
	RES
	XML_RES
	XML_CLASS
	ROUTE_CONTENT
	ROUTE_HOST
	OPENS
	INJECT // a field whose generic type a class implements: it can hold an instance from DI
)

const (
	accEnum      = 0x4000
	accAbstract  = 0x0400
	accInterface = 0x0200
	constantFlag = 0x10000
)

// Set by applyConfig from codemap.yml.
var (
	cfg                                       *Config
	classDirs, sourceSets, manifests          []string
	activityBases, dialogBases, fragmentBases map[string]bool
	previewMarkers                            map[string]bool
	screenLayout                              *regexp.Regexp
	routeRegistration, routeByName            *regexp.Regexp
	routeGraph                                *regexp.Regexp
	routeBases, childBases, routeInterfaces   map[string]bool
	routeReadOnly                             *regexp.Regexp
	startDestination                          string
	typedRoutes, stringRoutes                 bool
	keepStrings                               bool // classfile keeps string constants (stringRoutes)
)

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

var (
	routeNamePass = regexp.MustCompile(`^(kotlin/coroutines/jvm/internal/|kotlin/jvm/internal/Intrinsics\.` +
		`|kotlin/NoWhenBranchMatchedException\.)|\.access\$get`)
	mangled  = regexp.MustCompile(`-[\w]{7}$`)
	ktSuffix = regexp.MustCompile(`Kt$`)
)

var skipDirs = set("build", "src", "node_modules")

func opensRoute(owner, name, caller string) bool {
	if routeRegistration.MatchString(owner + "." + name) {
		return false
	}
	return !routeReadOnly.MatchString(name) || name == "getRoute" && caller == startDestination
}

// rClassType: `type` of an R class `pkg/R$type`, "" for any other class (`^.*/R\$(\w+)$`).
func rClassType(owner string) string {
	i := strings.LastIndex(owner, "/R$")
	if i < 0 || i+3 == len(owner) {
		return ""
	}
	t := owner[i+3:]
	for j := 0; j < len(t); j++ {
		c := t[j]
		if !(c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return ""
		}
	}
	return t
}

func normalizedName(name string) string {
	name = mangled.ReplaceAllString(name, "")
	return strings.TrimSuffix(name, "$default")
}

func inlinedLambdaOfSameFile(t, name string) bool {
	t0, _, _ := strings.Cut(t, "$")
	n0, _, _ := strings.Cut(name, "$")
	return t0 == n0 && strings.Contains(t, "$") && (strings.Contains(t, "$inlined$") || strings.Contains(t, "$lambda"))
}

func dotted(s string) string { return strings.NewReplacer("/", ".", "$", ".").Replace(s) }

func shortName(s string) string { return s[strings.LastIndexByte(s, '/')+1:] }

func descriptorClasses(desc string) []string {
	var out []string
	i := strings.IndexByte(desc, 'L')
	for i >= 0 {
		end := strings.IndexByte(desc[i:], ';')
		if end < 0 {
			break
		}
		end += i
		name := desc[i+1 : end]
		if lt := strings.IndexByte(name, '<'); lt >= 0 {
			out = append(out, name[:lt])
			i = indexFrom(desc, 'L', i+1+lt)
			continue
		}
		if name != "" && !strings.ContainsAny(name, "();[:") {
			out = append(out, name)
		}
		i = indexFrom(desc, 'L', end)
	}
	return out
}

func indexFrom(s string, c byte, from int) int {
	if from >= len(s) {
		return -1
	}
	if j := strings.IndexByte(s[from:], c); j >= 0 {
		return from + j
	}
	return -1
}

func nodeID(kind, owner, name, desc string) int64 {
	h, _ := blake2b.New(8, nil)
	h.Write([]byte(kind + "\x00" + owner + "\x00" + name + "\x00" + desc))
	v := int64(binary.BigEndian.Uint64(h.Sum(nil)) >> 1)
	if v == 0 {
		return 1
	}
	return v
}

// ---------------------------------------------------------------- modules

type Module struct {
	Name, Dir string
	ClassDirs []string // absolute
	// relative to root, in lookup order
	Sources   []string `json:"sources"`
	Res       []string `json:"res"`
	Manifests []string `json:"manifests"`
}

// templateModule: the module at rel with the paths of the source sets in the settings.
func templateModule(root, rel string, classes []string) Module {
	m := Module{Name: rel, Dir: rel, ClassDirs: classes}
	for _, s := range sourceSets {
		for _, lang := range []string{"kotlin", "java"} {
			m.Sources = append(m.Sources, filepath.Join(rel, "src", s, lang))
		}
		m.Manifests = append(m.Manifests, filepath.Join(rel, "src", s, "AndroidManifest.xml"))
	}
	for _, d := range resDirs(filepath.Join(root, rel)) {
		r, _ := filepath.Rel(root, d)
		m.Res = append(m.Res, r)
	}
	return m
}

func discoverModules(root string) []Module {
	var out []Module
	var walk func(d string)
	walk = func(d string) {
		ents, err := os.ReadDir(d)
		if err != nil {
			return
		}
		hasSrc := false
		var subs []string
		for _, e := range ents {
			isd := e.IsDir()
			if e.Type()&os.ModeSymlink != 0 {
				isd = isDir(filepath.Join(d, e.Name()))
			}
			if !isd {
				continue
			}
			if e.Name() == "src" {
				hasSrc = true
			}
			if !skipDirs[e.Name()] && !strings.HasPrefix(e.Name(), ".") && e.Type()&os.ModeSymlink == 0 {
				subs = append(subs, e.Name())
			}
		}
		var dirs []string
		any := false
		for _, c := range classDirs {
			p := filepath.Join(d, c)
			dirs = append(dirs, p)
			any = any || isDir(p)
		}
		if hasSrc && any {
			rel, _ := filepath.Rel(root, d)
			out = append(out, templateModule(root, rel, dirs))
		}
		for _, s := range subs {
			walk(filepath.Join(d, s))
		}
	}
	walk(root)
	return out
}

// ---------------------------------------------------------------- state

type Node struct {
	Kind, Name, Desc string // Desc "": none
	Owner, Top       int64  // 0: none
	Flags            int
	File, Module     int // 0: none
	LMin, LMax       int // 0: none
	Sname            string
}

type Edge struct {
	Src, Dst int64
	Kind     int
}

type Stamp struct {
	Size, Mtime int64
	Sha1        string
}

type File struct {
	ID, Module, Lines int // Lines -1: unreadable
}

type CallVia struct {
	Src, Dst int64
	Via      string
}

type CallSite struct {
	Src, Dst  int64
	Site, Pos int
	Kind      string
	Arg       int64
}

type Triple struct{ A, B, C int64 }

type Guarded struct {
	Method, Dst int64
	Kind        int
	Pos, Bit    int
}

type Screen struct {
	Node                int64
	Kind, Label, Target string
	Host                int64
}

type ResDef struct {
	Node int64
	File int
	Line int
}

type Annot struct {
	Node int64
	Name string
}

type Super struct {
	Cls  int64
	Sup  string
	Kind int
}

type EntryRow struct {
	Node         int64
	Kind, Detail string
	File, Line   int
}

type Parsed struct {
	Module int
	C      *Class
}

type Builder struct {
	root    string
	modules []Module
	jobs    int
	log     func(string)

	nodes         map[int64]*Node
	edges         map[Edge]struct{}
	callVia       map[CallVia]struct{}
	classID       map[string]int64
	classOrder    []string // class_id insertion order
	memberID      map[[3]string]int64
	memberOrder   [][3]string
	resID         map[string]int64
	resOrder      []string
	files         map[string]*File
	fileOrder     []string
	annots        []Annot
	supers        []Super
	screens       []*Screen
	resDefs       []ResDef
	related       map[[2]int64]struct{}
	entries       map[EntryRow]struct{}
	callSites     []CallSite
	paramSwitches map[Triple]struct{}
	directReads   map[[2]int64]struct{}
	defaultGuards []Guarded
	stats         map[string]string
	owned         map[int64][]int64
	inputs        map[string]Stamp
	sources       map[string]Stamp

	parsed                                            map[string]Parsed
	parsedNames                                       []string
	classTables                                       map[string][]tableSet
	routeClasses, childClasses, navTypes, enumClasses map[string]bool
	genericImpls                                      map[string][]string // `S<A>` -> the classes implementing it
	tables                                            map[[2]string]*tableMap
	sourceNames                                       map[string]map[string][]string
	srcMu                                             sync.Mutex
	lineCache                                         map[string]int

	childStart    map[string]string
	routeOpens    map[[2]any]struct{}
	routeOpenList []routeOpen
	classLiterals map[[2]int64]struct{}
	registrations []registration
	screenCls     map[int64]string
	inits         map[int64][]int64
}

type tableSet struct {
	key   [3]string
	index int
}

type tableMap struct {
	keys  [][3]string
	index map[[3]string]int
}

type routeOpen struct {
	method int64
	route  string
}

type registration struct {
	method  int64
	route   string
	content []content
}

type content struct {
	target int64
	kind   int
}

func NewBuilder(root string, modules []Module, jobs int, log func(string)) *Builder {
	if jobs <= 0 {
		jobs = max(1, runtime.NumCPU()-1)
	}
	return &Builder{root: root, modules: modules, jobs: jobs, log: log,
		nodes: make(map[int64]*Node, 1<<19), edges: make(map[Edge]struct{}, 1<<20), callVia: map[CallVia]struct{}{},
		classID: make(map[string]int64, 1<<16), memberID: make(map[[3]string]int64, 1<<19), resID: map[string]int64{},
		files: map[string]*File{}, related: map[[2]int64]struct{}{}, entries: map[EntryRow]struct{}{},
		paramSwitches: map[Triple]struct{}{}, directReads: map[[2]int64]struct{}{}, stats: map[string]string{},
		owned: make(map[int64][]int64, 1<<16), sourceNames: map[string]map[string][]string{}}
}

func (b *Builder) addNode(n *Node) int64 {
	owner := ""
	if n.Owner != 0 {
		owner = b.nodes[n.Owner].Name
	}
	return b.addNodeID(nodeID(n.Kind, owner, n.Name, n.Desc), n)
}

func (b *Builder) addNodeID(id int64, n *Node) int64 {
	if old, ok := b.nodes[id]; ok {
		fail("code map: node id collision: %v and %v", [3]string{old.Kind, old.Name, old.Desc}, [3]string{n.Kind, n.Name, n.Desc})
	}
	b.nodes[id] = n
	if n.Owner != 0 {
		b.owned[n.Owner] = append(b.owned[n.Owner], id)
	}
	return id
}

func (b *Builder) fileID(rel string, module int) int {
	f := b.files[rel]
	if f == nil {
		lines, ok := b.lineCache[rel]
		if !ok {
			lines = lineCount(filepath.Join(b.root, rel))
		}
		f = &File{len(b.files) + 1, module, lines}
		b.files[rel] = f
		b.fileOrder = append(b.fileOrder, rel)
	}
	return f.ID
}

func lineCount(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	return bytes.Count(data, []byte("\n")) + 1
}

func (b *Builder) sourceOf(m *Module, pkg, source string) string {
	for _, dir := range m.Sources {
		rel := filepath.Join(dir, pkg, source)
		if _, err := os.Stat(filepath.Join(b.root, rel)); err == nil {
			return rel
		}
	}
	re := regexp.MustCompile(`(?m)^\s*package\s+` + regexp.QuoteMeta(strings.ReplaceAll(pkg, "/", ".")) + `\s*;?\s*$`)
	for _, rel := range b.sourcesNamed(m)[source] {
		data, err := os.ReadFile(filepath.Join(b.root, rel))
		if err == nil && re.Match(data) {
			return rel
		}
	}
	return ""
}

func (b *Builder) sourcesNamed(m *Module) map[string][]string {
	b.srcMu.Lock()
	defer b.srcMu.Unlock()
	if names, ok := b.sourceNames[m.Dir]; ok {
		return names
	}
	out := map[string][]string{}
	for _, dir := range m.Sources {
		filepath.WalkDir(filepath.Join(b.root, dir), func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				rel, _ := filepath.Rel(b.root, p)
				out[d.Name()] = append(out[d.Name()], rel)
			}
			return nil
		})
	}
	b.sourceNames[m.Dir] = out
	return out
}

// ---------------------------------------------------------------- build

type parseResult struct {
	path   string
	module int
	cls    *Class
	stamp  Stamp
	err    error
}

func stampOf(data []byte, st os.FileInfo) Stamp {
	s := sha1.Sum(data)
	return Stamp{st.Size(), st.ModTime().UnixNano(), hex.EncodeToString(s[:])}
}

func readStamp(path string) (Stamp, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return Stamp{}, nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Stamp{}, nil, err
	}
	data := make([]byte, st.Size())
	if _, err := f.ReadAt(data, 0); err != nil && st.Size() > 0 {
		return Stamp{}, nil, err
	}
	return stampOf(data, st), data, nil
}

func parallel(n, jobs int, fn func(i int)) {
	var wg sync.WaitGroup
	next := make(chan int, 256)
	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
}

func (b *Builder) Build() {
	t0 := time.Now()
	var files []parseResult
	for i, m := range b.modules {
		for _, cdir := range m.ClassDirs {
			if !isDir(cdir) {
				continue
			}
			filepath.WalkDir(cdir, func(p string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".class") && d.Name() != "module-info.class" {
					files = append(files, parseResult{path: p, module: i + 1})
				}
				return nil
			})
		}
	}
	parallel(len(files), b.jobs, func(i int) {
		r := &files[i]
		st, data, err := readStamp(r.path)
		if err != nil {
			r.err = err
			return
		}
		r.stamp = st
		r.cls, r.err = Parse(data)
	})
	b.inputs = map[string]Stamp{}
	b.parsed = map[string]Parsed{}
	errors := 0
	for _, r := range files {
		if r.err != nil {
			errors++
			continue
		}
		rel, _ := filepath.Rel(b.root, r.path)
		b.inputs[rel] = r.stamp
		b.parsed[r.cls.Name] = Parsed{r.module, r.cls}
	}
	b.parsedNames = make([]string, 0, len(b.parsed))
	for n := range b.parsed {
		b.parsedNames = append(b.parsedNames, n)
	}
	sort.Strings(b.parsedNames)
	watched := watchedFiles(b.root, b.modules)
	stamps := make([]Stamp, len(watched))
	parallel(len(watched), b.jobs, func(i int) { stamps[i], _, _ = readStamp(filepath.Join(b.root, watched[i])) })
	b.sources = map[string]Stamp{}
	for i, w := range watched {
		b.sources[w] = stamps[i]
	}
	b.stats["source_files"] = fmt.Sprint(len(b.sources))
	b.stats["class_files"] = fmt.Sprint(len(files))
	b.stats["parse_errors"] = fmt.Sprint(errors)
	b.stats["parse_s"] = fmt.Sprintf("%.1f", time.Since(t0).Seconds())
	b.timed("parse", t0)
	t := time.Now()
	b.classes()
	b.members()
	b.timed("classes+members", t)
	t = time.Now()
	b.edgesAll()
	b.timed("edges", t)
	t = time.Now()
	b.resources()
	b.timed("resources", t)
	t = time.Now()
	b.screensAll()
	b.timed("screens", t)
	b.stats["build_s"] = fmt.Sprintf("%.1f", time.Since(t0).Seconds())
}

var verbose = os.Getenv("CODEMAP_TIMES") != ""

func (b *Builder) timed(stage string, t time.Time) {
	if verbose {
		fmt.Fprintf(os.Stderr, "  %-16s %.3f s\n", stage, time.Since(t).Seconds())
	}
}

// -- classes and files

func (b *Builder) classes() {
	type srcKey struct {
		module      int
		pkg, source string
	}
	srcCache := map[srcKey]string{}
	var keys []srcKey
	for _, name := range b.parsedNames {
		p := b.parsed[name]
		if p.C.Source == "" {
			continue
		}
		pkg := ""
		if i := strings.LastIndexByte(name, '/'); i >= 0 {
			pkg = name[:i]
		}
		k := srcKey{p.Module, pkg, p.C.Source}
		if _, ok := srcCache[k]; !ok {
			srcCache[k] = ""
			keys = append(keys, k)
		}
	}
	found := make([]string, len(keys))
	parallel(len(keys), b.jobs, func(i int) {
		k := keys[i]
		found[i] = b.sourceOf(&b.modules[k.module-1], k.pkg, k.source)
	})
	for i, k := range keys {
		srcCache[k] = found[i]
	}
	for _, name := range b.parsedNames {
		p := b.parsed[name]
		pkg := ""
		if i := strings.LastIndexByte(name, '/'); i >= 0 {
			pkg = name[:i]
		}
		rel := ""
		if p.C.Source != "" {
			rel = srcCache[srcKey{p.Module, pkg, p.C.Source}]
		}
		fid := 0
		if rel != "" {
			fid = b.fileID(rel, p.Module)
		}
		id := b.addNode(&Node{Kind: "C", Name: name, Flags: int(p.C.Flags), File: fid, Module: p.Module, Sname: dotted(shortName(name))})
		b.classID[name] = id
		b.classOrder = append(b.classOrder, name)
	}
	for _, name := range b.classOrder {
		cid := b.classID[name]
		n := b.nodes[cid]
		top, _, _ := strings.Cut(name, "$")
		if t, ok := b.classID[top]; ok {
			n.Top = t
		} else {
			n.Top = cid
		}
		if n.File == 0 {
			if tn := b.nodes[n.Top]; tn.File != 0 {
				n.File = tn.File
			}
		}
	}
	for _, name := range b.parsedNames {
		c := b.parsed[name].C
		cid := b.classID[name]
		if c.Super != "" {
			b.supers = append(b.supers, Super{cid, c.Super, EXTENDS})
		}
		for _, i := range c.Interfaces {
			b.supers = append(b.supers, Super{cid, i, IMPLEMENTS})
		}
		for _, a := range c.Annotations {
			b.annots = append(b.annots, Annot{cid, a})
		}
	}
}

// -- members

func (b *Builder) members() {
	fileLines := map[int]int{}
	for _, f := range b.files {
		fileLines[f.ID] = f.Lines
	}
	ids := make([][]int64, len(b.parsedNames))
	parallel(len(b.parsedNames), b.jobs, func(i int) {
		name := b.parsedNames[i]
		c := b.parsed[name].C
		out := make([]int64, 0, len(c.Fields)+len(c.Methods))
		for _, f := range c.Fields {
			out = append(out, nodeID("F", name, f.Name, f.Desc))
		}
		for _, m := range c.Methods {
			out = append(out, nodeID("M", name, m.Name, m.Desc))
		}
		ids[i] = out
	})
	for ci, name := range b.parsedNames {
		next := ids[ci]
		p := b.parsed[name]
		cid := b.classID[name]
		cn := b.nodes[cid]
		nLines, known := fileLines[cn.File]
		if known && nLines < 0 {
			known = false
		}
		for _, f := range p.C.Fields {
			flags := int(f.Flags)
			if f.Constant {
				flags |= constantFlag
			}
			id := b.addNodeID(next[0], &Node{Kind: "F", Name: f.Name, Desc: f.Desc, Owner: cid, Top: cn.Top, Flags: flags, File: cn.File, Module: p.Module, Sname: f.Name})
			next = next[1:]
			b.setMember([3]string{name, f.Name, f.Desc}, id)
		}
		for _, m := range p.C.Methods {
			lmin, lmax := 0, 0
			for _, x := range m.Lines {
				if known && x > nLines {
					continue
				}
				if lmin == 0 && lmax == 0 || x < lmin {
					lmin = x
				}
				if x > lmax {
					lmax = x
				}
			}
			id := b.addNodeID(next[0], &Node{Kind: "M", Name: m.Name, Desc: m.Desc, Owner: cid, Top: cn.Top, Flags: int(m.Flags), File: cn.File, Module: p.Module, LMin: lmin, LMax: lmax, Sname: normalizedName(m.Name)})
			next = next[1:]
			b.setMember([3]string{name, m.Name, m.Desc}, id)
			for _, a := range m.Annotations {
				b.annots = append(b.annots, Annot{id, a})
			}
		}
	}
}

func (b *Builder) setMember(k [3]string, id int64) {
	if _, ok := b.memberID[k]; !ok {
		b.memberOrder = append(b.memberOrder, k)
	}
	b.memberID[k] = id
}

func (b *Builder) resolveMember(owner, name, desc string, field bool) int64 {
	seen := map[string]bool{}
	stack := []string{owner}
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[c] {
			continue
		}
		if _, ok := b.classID[c]; !ok {
			continue
		}
		seen[c] = true
		if id, ok := b.memberID[[3]string{c, name, desc}]; ok {
			return id
		}
		info := b.parsed[c].C
		if info.Super != "" {
			stack = append(stack, info.Super)
		}
		if !field {
			stack = append(stack, info.Interfaces...)
		}
	}
	return 0
}

func (b *Builder) subclassesOf(bases map[string]bool) map[string]bool {
	memo := map[string]bool{}
	var check func(c string, depth int) bool
	check = func(c string, depth int) bool {
		if bases[c] {
			return true
		}
		if v, ok := memo[c]; ok {
			return v
		}
		memo[c] = false
		p, ok := b.parsed[c]
		r := ok && p.C.Super != "" && depth < 40 && check(p.C.Super, depth+1)
		memo[c] = r
		return r
	}
	out := map[string]bool{}
	for _, c := range b.parsedNames {
		if check(c, 0) {
			out[c] = true
		}
	}
	return out
}

// -- edges from code

type part struct {
	edges         []Edge
	callVia       []CallVia
	paramSwitches []Triple
	directReads   [][2]int64
	routeOpens    []routeOpen
	classLiterals [][2]int64
	resKeys       []string
	defaultGuards []Guarded
	callSites     []CallSite
	registrations []registration
	childStart    [][2]string
}

func (b *Builder) edgesAll() {
	b.classTables = map[string][]tableSet{}
	for _, n := range b.parsedNames {
		var ts []tableSet
		for _, m := range b.parsed[n].C.Methods {
			for _, r := range m.Refs {
				if r.Kind == rTableSet {
					ts = append(ts, tableSet{[3]string{r.Owner, r.Name, r.Desc}, r.Index})
				}
			}
		}
		b.classTables[n] = ts
	}
	b.routeClasses = b.subclassesOf(routeBases)
	b.childClasses = b.subclassesOf(childBases)
	b.navTypes = map[string]bool{}
	for _, m := range []map[string]bool{b.routeClasses, b.childClasses, routeInterfaces} {
		for k := range m {
			b.navTypes[k] = true
		}
	}
	b.tables = map[[2]string]*tableMap{}
	for _, n := range b.parsedNames {
		for _, t := range b.classTables[n] {
			k := [2]string{n, t.key[0]}
			tm := b.tables[k]
			if tm == nil {
				tm = &tableMap{index: map[[3]string]int{}}
				b.tables[k] = tm
			}
			if _, ok := tm.index[t.key]; !ok {
				tm.keys = append(tm.keys, t.key)
			}
			tm.index[t.key] = t.index
		}
	}
	b.enumClasses = map[string]bool{}
	for _, n := range b.parsedNames {
		if b.parsed[n].C.Flags&accEnum != 0 {
			b.enumClasses[n] = true
		}
	}
	b.genericImpls = map[string][]string{}
	for _, n := range b.parsedNames {
		if b.parsed[n].C.Enclosing[0] != "" { // a lambda or an anonymous class: not from DI
			continue
		}
		for _, t := range genericSupers(b.parsed[n].C.Signature) {
			if !strings.HasPrefix(t, "Ljava/") && !strings.HasPrefix(t, "Lkotlin/") { // Function1<A, B>, Comparable<A>
				b.genericImpls[t] = append(b.genericImpls[t], n)
			}
		}
	}
	parts := make([]*part, len(b.parsedNames))
	chunks := b.jobs * 4
	size := max(1, (len(b.parsedNames)+chunks-1)/chunks)
	nChunks := (len(b.parsedNames) + size - 1) / size
	parallel(nChunks, b.jobs, func(ci int) {
		w := &edgeWorker{b: b, byName: map[string]map[string][]int64{}}
		for i := ci * size; i < min(len(b.parsedNames), (ci+1)*size); i++ {
			parts[i] = w.edgesOf(b.parsedNames[i])
		}
	})
	b.childStart = map[string]string{}
	b.routeOpens = map[[2]any]struct{}{}
	b.classLiterals = map[[2]int64]struct{}{}
	for _, p := range parts {
		b.mergeEdges(p)
	}
}

func (b *Builder) resNode(key string) int64 {
	if id, ok := b.resID[key]; ok {
		return id
	}
	id := b.addNode(&Node{Kind: "R", Name: key, Sname: key})
	b.resID[key] = id
	b.resOrder = append(b.resOrder, key)
	return id
}

func (b *Builder) mergeEdges(p *part) {
	ids := make([]int64, len(p.resKeys))
	for i, k := range p.resKeys {
		ids[i] = b.resNode(k)
	}
	fix := func(n int64) int64 {
		if n < 0 {
			return ids[-n-1]
		}
		return n
	}
	for _, e := range p.edges {
		b.edges[Edge{e.Src, fix(e.Dst), e.Kind}] = struct{}{}
	}
	for _, g := range p.defaultGuards {
		g.Dst = fix(g.Dst)
		b.defaultGuards = append(b.defaultGuards, g)
	}
	b.callSites = append(b.callSites, p.callSites...)
	for _, v := range p.callVia {
		b.callVia[v] = struct{}{}
	}
	for _, v := range p.paramSwitches {
		b.paramSwitches[v] = struct{}{}
	}
	for _, v := range p.directReads {
		b.directReads[v] = struct{}{}
	}
	for _, v := range p.routeOpens {
		k := [2]any{v.method, v.route}
		if _, ok := b.routeOpens[k]; !ok {
			b.routeOpens[k] = struct{}{}
			b.routeOpenList = append(b.routeOpenList, v)
		}
	}
	for _, v := range p.classLiterals {
		b.classLiterals[v] = struct{}{}
	}
	b.registrations = append(b.registrations, p.registrations...)
	for _, v := range p.childStart {
		b.childStart[v[0]] = v[1]
	}
}

type edgeWorker struct {
	b      *Builder
	byName map[string]map[string][]int64
}

// ordered set helper
type oset[K comparable] struct {
	keys []K
	seen map[K]struct{}
}

func (s *oset[K]) add(k K) {
	if s.seen == nil {
		s.seen = map[K]struct{}{}
	}
	if _, ok := s.seen[k]; !ok {
		s.seen[k] = struct{}{}
		s.keys = append(s.keys, k)
	}
}

type pb struct{ p, b int }

func (w *edgeWorker) edgesOf(name string) *part {
	b := w.b
	var edges oset[Edge]
	var callVia oset[CallVia]
	var paramSwitches oset[Triple]
	var directReads oset[[2]int64]
	var routeOpens oset[routeOpen]
	var classLiterals oset[[2]int64]
	resKeys := map[string]int64{}
	var resOrder []string
	out := &part{}
	resRef := func(key string) int64 {
		if id, ok := resKeys[key]; ok {
			return id
		}
		id := -int64(len(resKeys)) - 1
		resKeys[key] = id
		resOrder = append(resOrder, key)
		return id
	}
	p := b.parsed[name]
	info := p.C
	cid := b.classID[name]
	var mid int64
	var edgeGuards map[[2]int64]map[pb]bool // nil value: unguarded code
	var egOrder [][2]int64
	var guard map[pb]bool
	hasGuards := false
	add := func(e Edge) {
		edges.add(e)
		if hasGuards && e.Src == mid {
			k := [2]int64{e.Dst, int64(e.Kind)}
			old, in := edgeGuards[k]
			if !in {
				egOrder = append(egOrder, k)
			}
			if guard == nil || (in && old == nil) {
				edgeGuards[k] = nil
			} else {
				m := map[pb]bool{}
				for x := range old {
					m[x] = true
				}
				for x := range guard {
					m[x] = true
				}
				edgeGuards[k] = m
			}
		}
	}
	if id, ok := b.classID[info.Super]; ok {
		add(Edge{cid, id, EXTENDS})
	}
	for _, i := range info.Interfaces {
		if id, ok := b.classID[i]; ok {
			add(Edge{cid, id, IMPLEMENTS})
		}
	}
	for _, f := range info.Fields {
		fid := b.memberID[[3]string{name, f.Name, f.Desc}]
		for _, t := range descriptorClasses(f.Desc) {
			if id, ok := b.classID[t]; ok && t != name {
				add(Edge{fid, id, TYPE})
			}
		}
		if f.Signature != "" {
			for _, t := range b.genericImpls[strings.ReplaceAll(f.Signature, "+L", "L")] {
				if id, ok := b.classID[t]; ok && t != name {
					add(Edge{fid, id, INJECT})
				}
			}
		}
	}
	var smap []string
	for _, c := range info.Smap {
		if _, ok := b.classID[c]; ok {
			smap = append(smap, c)
		}
	}
	siteNo := 0
	for mi := range info.Methods {
		m := &info.Methods[mi]
		mid = b.memberID[[3]string{name, m.Name, m.Desc}]
		hasGuards = len(m.Guards) > 0
		edgeGuards, egOrder = nil, nil
		if hasGuards {
			edgeGuards = map[[2]int64]map[pb]bool{}
		}
		guard = nil
		for _, t := range descriptorClasses(m.Desc) {
			if id, ok := b.classID[t]; ok && t != name {
				add(Edge{mid, id, TYPE})
			}
		}
		initializer := m.Name == "<init>" || m.Name == "<clinit>"
		generated := strings.HasSuffix(name, "$WhenMappings") && m.Name == "<clinit>"
		var pending []content
		route := ""
		var cont []content
		var routeReads []string
		var named []string
		var strs []string
		reified := false // the next class literal is a type parameter of an inline function's own body
		for i := range m.Refs {
			ref := &m.Refs[i]
			if hasGuards {
				guard = nil
				if gs, ok := m.Guards[i]; ok {
					guard = map[pb]bool{}
					for _, g := range gs {
						guard[pb{g.Param, g.Bit}] = true
					}
				}
			}
			if generated || ref.Kind == rTableSet {
				continue
			}
			switch ref.Kind {
			case rString:
				strs = append(strs, ref.Owner)
				continue
			case rTableRead:
				if tm := b.tables[[2]string{ref.Owner, ref.Name}]; tm != nil {
					for _, k := range tm.keys {
						index := tm.index[k]
						target := b.resolveMember(k[0], k[1], k[2], true)
						in := false
						for _, c := range ref.Cases {
							if int(c) == index {
								in = true
							}
						}
						if in && target != 0 {
							add(Edge{mid, target, READ})
							if ref.Param >= 0 {
								paramSwitches.add(Triple{mid, target, int64(ref.Param)})
							}
						}
					}
				}
				continue
			case rType, rClassLit:
				t := ref.Owner
				tid, ok := b.classID[t]
				if !ok || t == name {
					continue
				}
				if ref.Kind == rClassLit && !strings.HasSuffix(m.Desc, ")Z") {
					classLiterals.add([2]int64{mid, tid})
				}
				if ref.Kind == rClassLit && reified {
					reified = false
				} else if (ref.Kind == rClassLit || ref.Index == 0xC1) && typedRoutes && b.routeCandidate(t) {
					route = t // composable<T> or entry<T>, or `is T -> NavEntry(key)`
				}
				if initializer && strings.HasPrefix(t, name+"$") {
					pending = append(pending, content{tid, TYPE})
				} else {
					add(Edge{mid, tid, TYPE})
				}
				if strings.HasPrefix(t, name+"$") || inlinedLambdaOfSameFile(t, name) {
					cont = append(cont, content{tid, TYPE})
				}
			case rLambda:
				target, ok := b.memberID[[3]string{ref.Owner, ref.Name, ref.Desc}]
				if !ok {
					continue
				}
				if initializer && ref.Owner == name {
					pending = append(pending, content{target, LAMBDA})
				} else {
					add(Edge{mid, target, LAMBDA})
				}
				cont = append(cont, content{target, LAMBDA})
			case rInline:
				for _, c := range smap {
					for _, target := range w.membersByName(c, ref.Name) {
						add(Edge{mid, target, INLINE})
					}
				}
			default:
				owner, n, d := ref.Owner, ref.Name, ref.Desc
				call := ref.Kind == rCall
				full := owner + "." + n
				if call && n == "reifiedOperationMarker" && owner == "kotlin/jvm/internal/Intrinsics" {
					reified = true
				}
				var popped []string
				if call && len(strs) > 0 {
					if k := min(stringParams(d), len(strs)); k > 0 {
						popped = strs[len(strs)-k:]
						strs = strs[:len(strs)-k]
					}
				}
				if call && routeGraph.MatchString(full) {
					if len(popped) >= 2 {
						out.childStart = append(out.childStart, [2]string{`"` + popped[1], `"` + popped[0]})
					} else if typedRoutes && route != "" && len(routeReads) > 0 {
						out.childStart = append(out.childStart, [2]string{route, routeReads[len(routeReads)-1]})
						routeReads = routeReads[:len(routeReads)-1]
					}
					route = ""
				}
				if call && routeRegistration.MatchString(full) {
					if route == "" && stringRoutes && len(popped) > 0 {
						route = `"` + popped[0]
					} else if route == "" && typedRoutes && len(routeReads) > 0 {
						route = routeReads[len(routeReads)-1]
						routeReads = routeReads[:len(routeReads)-1]
					}
					if route != "" {
						out.registrations = append(out.registrations, registration{mid, route, cont})
					}
					route = ""
					cont = nil
				}
				if (typedRoutes || stringRoutes) && call && len(cont) > 0 && strings.Contains(d, "Lkotlin/jvm/functions/Function") &&
					!routeRegistration.MatchString(full) {
					// lambdas handed to the app's own function (a graph section's onClick) are not the
					// content of the next destination
					if _, ok := b.classID[owner]; ok {
						cont = nil
					}
				}
				if len(named) > 0 && call && n != "getRoute" && !routeNamePass.MatchString(full) {
					if routeByName.MatchString(full) {
						for _, r := range named {
							routeOpens.add(routeOpen{mid, r})
						}
					}
					named = nil
				}
				if len(popped) > 0 && routeByName.MatchString(full) && opensRoute(owner, n, m.Name) {
					for _, s := range popped {
						routeOpens.add(routeOpen{mid, `"` + s})
					}
				}
				if len(routeReads) > 0 && call && typedRoutes && (n == "areEqual" || n == "equals" ||
					owner == routeReads[len(routeReads)-1] && n == "serializer") {
					// compared, or asked for its serializer: not handed anywhere
					routeReads = routeReads[:len(routeReads)-1]
				}
				if len(routeReads) > 0 && call {
					taken := 0
					if b.navTypes[owner] {
						taken++
					}
					if taken == 0 && typedRoutes && routeByName.MatchString(full) {
						taken = 1
					}
					for _, pd := range paramDescs(d) {
						if c := classOfDesc(pd); c != "" && b.navTypes[c] {
							taken++
						}
					}
					if taken > 0 || !typedRoutes && routeRegistration.MatchString(full) {
						k := min(max(taken, 1), len(routeReads))
						tk := append([]string(nil), routeReads[len(routeReads)-k:]...)
						routeReads = routeReads[:len(routeReads)-k]
						if n == "<init>" && childBases[owner] && b.childClasses[name] {
							out.childStart = append(out.childStart, [2]string{name, tk[len(tk)-1]})
						} else if opensRoute(owner, n, m.Name) {
							for _, r := range tk {
								routeOpens.add(routeOpen{mid, r})
							}
						} else if n == "getRoute" {
							named = append(named, tk...)
						}
					}
				}
				// the code of a route, of its nested classes (serializer) and of its sealed parent (whose
				// serializer lists the subclasses) makes the route without opening it
				typedOpen := typedRoutes && owner != name && !strings.HasPrefix(name, owner+"$") && !b.ancestorOf(name, owner)
				if typedOpen && call && n == "<init>" && !(m.Name == "<init>" && owner == info.Super) && b.routeCandidate(owner) {
					routeReads = append(routeReads, owner)
				}
				if rtype := rClassType(owner); rtype != "" && ref.Kind == rRead {
					add(Edge{mid, resRef(resKey(rtype, n)), RES})
					continue
				}
				if _, ok := b.classID[owner]; !ok {
					continue
				}
				if ref.Kind == rRead && n == "INSTANCE" && b.routeClasses[owner] {
					route = owner
				}
				if ref.Kind == rRead && n == "INSTANCE" && (b.navTypes[owner] || typedOpen && b.routeCandidate(owner)) {
					routeReads = append(routeReads, owner)
				}
				target := b.resolveMember(owner, n, d, !call)
				if target == 0 {
					continue
				}
				if ref.Kind == rWrite {
					if len(pending) > 0 && owner == name {
						for _, pc := range pending {
							add(Edge{target, pc.target, pc.kind})
						}
						pending = nil
					}
					add(Edge{mid, target, WRITE})
				} else if ref.Kind == rRead && initializer && owner != name && b.enumClasses[owner] && b.nodes[target].Flags&accEnum != 0 {
					pending = append(pending, content{target, READ})
				} else {
					k := READ
					if call {
						k = CALL
					}
					add(Edge{mid, target, k})
					if ref.Kind == rRead && b.enumClasses[owner] && b.nodes[target].Flags&accEnum != 0 {
						directReads.add([2]int64{mid, target})
					}
					if call {
						siteNo++
						w.siteArgs(&out.callSites, mid, target, d, ref.Args, siteNo, strings.HasSuffix(n, "$default"))
						if _, ok := b.memberID[[3]string{owner, n, d}]; !ok {
							callVia.add(CallVia{mid, target, owner})
						}
						if strings.HasPrefix(n, "getLambda$") && strings.HasPrefix(shortName(owner), "ComposableSingletons$") {
							cont = append(cont, content{target, CALL})
						}
					}
				}
			}
		}
		guard = nil
		for _, r := range routeReads {
			routeOpens.add(routeOpen{mid, r})
		}
		for _, pc := range pending {
			add(Edge{mid, pc.target, pc.kind})
			if pc.kind == READ {
				directReads.add([2]int64{mid, pc.target})
			}
		}
		for _, k := range egOrder {
			for x := range edgeGuards[k] {
				out.defaultGuards = append(out.defaultGuards, Guarded{mid, k[0], int(k[1]), x.p, x.b})
			}
		}
	}
	out.edges = edges.keys
	out.callVia = callVia.keys
	out.paramSwitches = paramSwitches.keys
	out.directReads = directReads.keys
	out.routeOpens = routeOpens.keys
	out.classLiterals = classLiterals.keys
	out.resKeys = resOrder
	return out
}

// routeCandidate: c may be a route of typedRoutes: a class of the map, not a lambda, a class the
// compiler made for one or a class declared inside a function.
func (b *Builder) routeCandidate(c string) bool {
	p, ok := b.parsed[c]
	if !ok || strings.Contains(c, "$$") || strings.Contains(c, "$lambda") ||
		strings.HasPrefix(shortName(c), "ComposableSingletons$") {
		return false
	}
	s := p.C.Super
	if s == "kotlin/jvm/internal/Lambda" || strings.HasPrefix(s, "kotlin/coroutines/jvm/internal/") || p.C.Enclosing[1] != "" {
		return false
	}
	for _, i := range p.C.Interfaces {
		if strings.HasPrefix(i, "kotlin/jvm/functions/Function") {
			return false
		}
	}
	return true
}

// ancestorOf: a is a superclass or an interface of c, within the map.
func (b *Builder) ancestorOf(a, c string) bool {
	for depth := 0; depth < 40; depth++ {
		p, ok := b.parsed[c]
		if !ok {
			return false
		}
		for _, i := range p.C.Interfaces {
			if i == a || b.ancestorOf(a, i) {
				return true
			}
		}
		if p.C.Super == a {
			return true
		}
		c = p.C.Super
	}
	return false
}

// stringParams: the number of String parameters in a method descriptor.
func stringParams(desc string) int {
	n := 0
	for _, pd := range paramDescs(desc) {
		if pd == "Ljava/lang/String;" {
			n++
		}
	}
	return n
}

func (w *edgeWorker) siteArgs(sites *[]CallSite, caller, callee int64, desc string, args []Arg, site int, defaults bool) {
	b := w.b
	for pos, pd := range paramDescs(desc) {
		var a Arg
		if args != nil {
			a = args[pos]
		}
		if defaults && pd == "I" {
			if a.K == 'i' {
				*sites = append(*sites, CallSite{caller, callee, site, pos, "i", int64(a.V)})
			} else {
				*sites = append(*sites, CallSite{caller, callee, site, pos, "u", 0})
			}
			continue
		}
		if !b.enumClasses[classOfDesc(pd)] {
			continue
		}
		kind, value := "u", int64(0)
		switch {
		case a.K == 'n':
			kind = "n"
		case a.K == 'p':
			kind, value = "p", int64(a.V)
		case a.K == 'c' && b.enumClasses[a.Owner]:
			if fid := b.resolveMember(a.Owner, a.N, a.D, true); fid != 0 && b.nodes[fid].Flags&accEnum != 0 {
				kind, value = "c", fid
			}
		}
		*sites = append(*sites, CallSite{caller, callee, site, pos, kind, value})
	}
}

func (w *edgeWorker) membersByName(cls, kotlinName string) []int64 {
	idx, ok := w.byName[cls]
	if !ok {
		b := w.b
		idx = map[string][]int64{}
		type mm struct {
			name, desc string
			id         int64
		}
		var ms []mm
		for _, v := range b.owned[b.classID[cls]] {
			n := b.nodes[v]
			ms = append(ms, mm{n.Name, n.Desc, v})
		}
		sort.Slice(ms, func(i, j int) bool {
			if ms[i].name != ms[j].name {
				return ms[i].name < ms[j].name
			}
			if ms[i].desc != ms[j].desc {
				return ms[i].desc < ms[j].desc
			}
			return ms[i].id < ms[j].id
		})
		for _, m := range ms {
			k := normalizedName(m.name)
			idx[k] = append(idx[k], m.id)
		}
		w.byName[cls] = idx
	}
	return idx[kotlinName]
}

// -- resources

func (b *Builder) resources() {
	byDotted := map[string]string{}
	for _, name := range b.classOrder {
		byDotted[dotted(name)] = name
	}
	suffixes := map[string][]string{}
	for _, name := range b.classOrder {
		d := dotted(name)
		if byDotted[d] != name {
			continue // a later class took this dotted name; Python keeps one entry per key
		}
		parts := strings.Split(d, ".")
		for i := 1; i < len(parts); i++ {
			k := strings.Join(parts[i:], ".")
			suffixes[k] = append(suffixes[k], name)
		}
	}
	n := 0
	scanned := make([][]*Resource, len(b.modules))
	parallel(len(b.modules), b.jobs, func(i int) { scanned[i] = scanModule(&b.modules[i], b.root) })
	lines := map[string]int{}
	var resFiles []string
	for _, rs := range scanned {
		for _, r := range rs {
			if _, ok := lines[r.File]; !ok {
				lines[r.File] = 0
				resFiles = append(resFiles, r.File)
			}
		}
	}
	counts := make([]int, len(resFiles))
	parallel(len(resFiles), b.jobs, func(i int) { counts[i] = lineCount(filepath.Join(b.root, resFiles[i])) })
	for i, f := range resFiles {
		lines[f] = counts[i]
	}
	b.lineCache = lines
	for i := range b.modules {
		for _, r := range scanned[i] {
			n++
			rid := b.resNode(r.Key)
			fid := b.fileID(r.File, i+1)
			node := b.nodes[rid]
			if node.Module == 0 {
				node.Module, node.File = i+1, fid
			}
			b.resDefs = append(b.resDefs, ResDef{rid, fid, r.Line})
			for _, k := range sortedKeys(r.Refs) {
				if k != r.Key {
					b.edges[Edge{rid, b.resNode(k), XML_RES}] = struct{}{}
				}
			}
			for _, c := range sortedKeys(r.Classes) {
				name, ok := byDotted[c]
				if !ok {
					cands := suffixes[strings.TrimLeft(c, ".")]
					if len(cands) == 1 {
						name, ok = cands[0], true
					}
				}
				if ok {
					b.edges[Edge{rid, b.classID[name], XML_CLASS}] = struct{}{}
				}
			}
		}
	}
	b.stats["resources"] = fmt.Sprint(n)
}

func sortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// -- screens

func (b *Builder) screensAll() {
	activities := b.subclassesOf(activityBases)
	fragments := b.subclassesOf(fragmentBases)
	dialogs := b.subclassesOf(dialogBases)
	b.screenCls = map[int64]string{}
	var names []string
	for _, n := range b.parsedNames {
		if activities[n] || fragments[n] {
			names = append(names, n)
		}
	}
	for _, name := range names {
		info := b.parsed[name].C
		short := shortName(name)
		if int(info.Flags)&(accAbstract|accInterface) != 0 || strings.HasPrefix(short, "Hilt_") || strings.Contains(name, "$") {
			continue
		}
		kind := "fragment"
		if activities[name] {
			kind = "activity"
		} else if dialogs[name] {
			kind = "dialog"
		}
		b.screenCls[b.classID[name]] = kind
		b.screens = append(b.screens, &Screen{b.classID[name], kind, dotted(short), dotted(name), 0})
	}
	previewAnn := map[string]bool{}
	for _, a := range b.annots {
		if previewMarkers[shortName(a.Name)] {
			previewAnn[a.Name] = true
		}
	}
	for _, a := range b.annots {
		if previewAnn[a.Name] && b.nodes[a.Node].Kind == "M" {
			m := b.nodes[a.Node]
			cls := b.nodes[m.Owner].Name
			fn := normalizedName(m.Name)
			short, _, _ := strings.Cut(shortName(cls), "$")
			top, _, _ := strings.Cut(cls, "$")
			b.screens = append(b.screens, &Screen{a.Node, "preview", ktSuffix.ReplaceAllString(short, "") + "." + fn,
				strings.ReplaceAll(top, "/", ".") + "." + fn, 0})
		}
	}
	for _, key := range b.resOrder {
		rid := b.resID[key]
		if screenLayout.MatchString(key) && b.nodes[rid].File != 0 {
			_, l, _ := strings.Cut(key, "/")
			b.screens = append(b.screens, &Screen{rid, "layout", l, l, 0})
		}
	}
	b.inits = map[int64][]int64{}
	for _, k := range b.memberOrder {
		if k[1] == "<init>" {
			c := b.classID[k[0]]
			b.inits[c] = append(b.inits[c], b.memberID[k])
		}
	}
	routeNodes := map[string]int64{}
	for _, r := range b.registrations {
		rnode, ok := routeNodes[r.route]
		if !ok {
			var label, qualified string
			var cn *Node
			if str, isStr := strings.CutPrefix(r.route, `"`); isStr {
				label, qualified = str, str
				cn = b.nodes[b.nodes[r.method].Top] // the class registering it
			} else {
				label, qualified = dotted(shortName(r.route)), dotted(r.route)
				cn = b.nodes[b.classID[r.route]]
			}
			rnode = b.addNode(&Node{Kind: "ROUTE", Name: r.route, Module: cn.Module, File: cn.File, Sname: label})
			routeNodes[r.route] = rnode
			b.screens = append(b.screens, &Screen{rnode, "route", label, qualified, 0})
		}
		for _, c := range r.content {
			delete(b.edges, Edge{r.method, c.target, c.kind})
			b.edges[Edge{rnode, c.target, ROUTE_CONTENT}] = struct{}{}
			if c.kind == TYPE {
				for _, init := range b.inits[c.target] {
					delete(b.edges, Edge{r.method, init, CALL})
				}
			}
		}
		b.edges[Edge{rnode, r.method, ROUTE_HOST}] = struct{}{}
	}
	var stringRoutes [][]string // segments, then the route key: a destination's or a nested graph's
	for k := range routeNodes {
		if str, ok := strings.CutPrefix(k, `"`); ok {
			stringRoutes = append(stringRoutes, append(routeSegments(str), k))
		}
	}
	for k := range b.childStart {
		if str, ok := strings.CutPrefix(k, `"`); ok {
			stringRoutes = append(stringRoutes, append(routeSegments(str), k))
		}
	}
	slices.SortFunc(stringRoutes, func(a, b []string) int { return strings.Compare(a[len(a)-1], b[len(b)-1]) })
	for _, ro := range b.routeOpenList {
		rc := ro.route
		if str, ok := strings.CutPrefix(rc, `"`); ok {
			segs := routeSegments(str)
			for _, sr := range stringRoutes {
				if !segmentsMatch(segs, sr[:len(sr)-1]) {
					continue
				}
				k := sr[len(sr)-1]
				if s, ok := b.childStart[k]; ok {
					k = s
				}
				if rn, ok := routeNodes[k]; ok {
					b.edges[Edge{ro.method, rn, OPENS}] = struct{}{}
				}
			}
			continue
		}
		if s, ok := b.childStart[rc]; ok {
			rc = s
		}
		if rn, ok := routeNodes[rc]; ok {
			b.edges[Edge{ro.method, rn, OPENS}] = struct{}{}
		}
	}
	for cl := range b.classLiterals {
		if b.screenCls[cl[1]] == "activity" {
			b.edges[Edge{cl[0], cl[1], OPENS}] = struct{}{}
		}
	}
	b.navGraphs()
	b.hosts()
	b.relatedAll()
	b.entriesAll()
}

// routeSegments: the path of a string route without its query, "*" for a {placeholder} or a part
// built at run time (\x01 or \x02 of a string template): "profile/{id}?tab={tab}" -> [profile *].
func routeSegments(route string) []string {
	if i := strings.IndexByte(route, '?'); i >= 0 {
		route = route[:i]
	}
	segs := strings.Split(route, "/")
	for i, s := range segs {
		if strings.ContainsAny(s, "{\x01\x02") {
			segs[i] = "*"
		}
	}
	return segs
}

// segmentsMatch: an opened route matches a registered one segment by segment; "*" matches any.
func segmentsMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] && a[i] != "*" && b[i] != "*" {
			return false
		}
	}
	return true
}

type navEdge struct {
	key, dest string
	g         *NavGraph
}

func (b *Builder) navGraphs() {
	dests, graphs := map[string]map[string]bool{}, map[string]map[string]bool{}
	addTo := func(m map[string]map[string]bool, k, v string) {
		if m[k] == nil {
			m[k] = map[string]bool{}
		}
		m[k][v] = true
	}
	var edges []navEdge
	for _, m := range b.modules {
		for _, rdir := range m.Res {
			rdir = filepath.Join(b.root, rdir)
			for _, sub := range listDir(rdir) {
				if resourceTypeOfDir(sub) != "navigation" {
					continue
				}
				for _, f := range listDir(filepath.Join(rdir, sub)) {
					if !strings.HasSuffix(f, ".xml") {
						continue
					}
					data, err := os.ReadFile(filepath.Join(rdir, sub, f))
					if err != nil {
						continue
					}
					g := parseNavigation(data)
					for _, k := range g.DestIDs {
						addTo(dests, k, g.Dests[k])
					}
					for _, k := range g.GraphIDs {
						if k != "" {
							addTo(graphs, k, g.Graphs[k])
						}
					}
					for _, d := range g.DestIDs {
						edges = append(edges, navEdge{"id/" + d, d, g})
					}
					for _, a := range g.Actions {
						edges = append(edges, navEdge{"id/" + a[0], a[1], g})
					}
					for _, k := range g.GraphIDs {
						if k != "" {
							edges = append(edges, navEdge{"id/" + k, k, g})
						}
					}
					if s, ok := g.Graphs[""]; ok {
						edges = append(edges, navEdge{"navigation/" + strings.TrimSuffix(f, ".xml"), s, g})
					}
				}
			}
		}
	}
	var resolve func(dest string, g *NavGraph, depth int) map[string]bool
	resolve = func(dest string, g *NavGraph, depth int) map[string]bool {
		if g != nil {
			if c, ok := g.Dests[dest]; ok {
				return map[string]bool{c: true}
			}
			if s, ok := g.Graphs[dest]; ok && depth < 10 {
				return resolve(s, g, depth+1)
			}
		}
		if d, ok := dests[dest]; ok {
			return d
		}
		out := map[string]bool{}
		if depth < 10 {
			for start := range graphs[dest] {
				for c := range resolve(start, nil, depth+1) {
					out[c] = true
				}
			}
		}
		return out
	}
	for _, e := range edges {
		rid, hasR := b.resID[e.key]
		for _, cls := range sortedKeys(resolve(e.dest, e.g, 0)) {
			cid, ok := b.classID[strings.ReplaceAll(cls, ".", "/")]
			if hasR && ok {
				if _, scr := b.screenCls[cid]; scr {
					b.edges[Edge{rid, cid, OPENS}] = struct{}{}
				}
			}
		}
	}
}

func (b *Builder) entriesAll() {
	b.entries = map[EntryRow]struct{}{}
	for i, m := range b.modules {
		for _, rel := range m.Manifests {
			path := filepath.Join(b.root, rel)
			if !isFile(path) {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			for _, e := range parseManifest(string(data)) {
				jvm := strings.ReplaceAll(e.Cls, ".", "/")
				cid, ok := b.classID[jvm]
				if !ok && strings.HasPrefix(e.Cls, ".") {
					var cands []int64
					for _, n := range b.classOrder {
						if strings.HasSuffix(n, jvm) {
							cands = append(cands, b.classID[n])
						}
					}
					if len(cands) == 1 {
						cid, ok = cands[0], true
					}
				}
				if ok && b.screenCls[cid] == "activity" {
					r, _ := filepath.Rel(b.root, path)
					fid := b.fileID(r, i+1)
					b.entries[EntryRow{cid, e.Kind, e.Detail, fid, e.Line}] = struct{}{}
				}
			}
		}
	}
}

func (b *Builder) relatedAll() {
	outEdges := map[int64][]Edge{}
	for e := range b.edges {
		outEdges[e.Src] = append(outEdges[e.Src], e)
	}
	previewsByFile := map[int][]int64{}
	layouts := map[int64]bool{}
	for _, s := range b.screens {
		switch s.Kind {
		case "preview":
			f := b.nodes[s.Node].File
			previewsByFile[f] = append(previewsByFile[f], s.Node)
		case "layout":
			layouts[s.Node] = true
		}
	}
	names := append([]string(nil), b.classOrder...)
	sort.Strings(names)
	nested := func(name string) []int64 {
		i := sort.SearchStrings(names, name)
		var out []int64
		for i < len(names) && (names[i] == name || strings.HasPrefix(names[i], name+"$")) {
			for _, m := range b.owned[b.classID[names[i]]] {
				if b.nodes[m].Kind == "M" {
					out = append(out, m)
				}
			}
			i++
		}
		return out
	}
	isComposable := func(id int64) bool {
		return strings.Contains(b.nodes[id].Desc, "Landroidx/compose/runtime/Composer;")
	}
	collect := func(screen int64, methods []int64, ownFile int) {
		module := b.nodes[screen].Module
		files := map[int]bool{}
		if ownFile != 0 {
			files[ownFile] = true
		}
		for _, m := range methods {
			for _, e := range outEdges[m] {
				d, k := e.Dst, e.Kind
				dn := b.nodes[d]
				if (k == CALL || k == LAMBDA) && dn.Kind == "M" && isComposable(d) && dn.Module == module {
					files[dn.File] = true
				} else if k == RES && layouts[d] {
					b.related[[2]int64{screen, d}] = struct{}{}
				} else if (k == TYPE || k == CALL) && (dn.Kind == "C" || dn.Kind == "M") {
					cn := d
					if dn.Kind == "M" {
						cn = dn.Owner
					}
					cname := b.nodes[cn].Name
					if strings.HasSuffix(cname, "Binding") && strings.Contains(cname, "/databinding/") {
						for _, bm := range nested(cname) {
							for _, e2 := range outEdges[bm] {
								if e2.Kind == RES && layouts[e2.Dst] {
									b.related[[2]int64{screen, e2.Dst}] = struct{}{}
								}
							}
						}
					}
				}
			}
		}
		for f := range files {
			for _, p := range previewsByFile[f] {
				b.related[[2]int64{screen, p}] = struct{}{}
			}
		}
	}
	for _, s := range b.screens {
		switch s.Kind {
		case "activity", "fragment", "dialog":
			collect(s.Node, nested(b.nodes[s.Node].Name), b.nodes[s.Node].File)
		case "route":
			var methods []int64
			for _, e := range outEdges[s.Node] {
				if e.Kind != ROUTE_CONTENT {
					continue
				}
				dn := b.nodes[e.Dst]
				if dn.Kind == "C" {
					methods = append(methods, nested(dn.Name)...)
				} else {
					methods = append(methods, e.Dst)
				}
			}
			collect(s.Node, methods, 0)
		}
	}
}

func (b *Builder) hosts() {
	rev := map[int64]map[int64]bool{}
	routeHost := map[int64][]int64{}
	for e := range b.edges {
		switch e.Kind {
		case CALL, RES, XML_RES, XML_CLASS, TYPE:
			if rev[e.Dst] == nil {
				rev[e.Dst] = map[int64]bool{}
			}
			rev[e.Dst][e.Src] = true
		case ROUTE_HOST:
			routeHost[e.Src] = append(routeHost[e.Src], e.Dst)
		}
	}
	activities := map[int64]bool{}
	for c, k := range b.screenCls {
		if k == "activity" {
			activities[c] = true
		}
	}
	keyCache := map[int64][4]string{}
	key := func(i int64) [4]string {
		if k, ok := keyCache[i]; ok {
			return k
		}
		n := b.nodes[i]
		o := ""
		if n.Owner != 0 {
			o = b.nodes[n.Owner].Name
		}
		k := [4]string{n.Kind, o, n.Name, n.Desc}
		keyCache[i] = k
		return k
	}
	order := func(ids []int64) []int64 {
		out := append([]int64(nil), ids...)
		sort.Slice(out, func(i, j int) bool {
			a, c := key(out[i]), key(out[j])
			for k := 0; k < 4; k++ {
				if a[k] != c[k] {
					return a[k] < c[k]
				}
			}
			return false
		})
		return out
	}
	keysOf := func(m map[int64]bool) []int64 {
		out := make([]int64, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		return out
	}
	findActivity := func(start int64, maxDepth int) int64 {
		seen := map[int64]bool{start: true}
		frontier := []int64{start}
		for d := 0; d < maxDepth; d++ {
			var nxt []int64
			for _, n := range frontier {
				top := b.nodes[n].Top
				if top == 0 {
					top = n
				}
				if activities[top] {
					return top
				}
				for _, u := range order(keysOf(rev[n])) {
					if !seen[u] {
						seen[u] = true
						nxt = append(nxt, u)
					}
				}
			}
			frontier = nxt
		}
		return 0
	}
	for _, s := range b.screens {
		switch {
		case s.Kind == "route" && routeHost[s.Node] != nil:
			for _, m := range order(routeHost[s.Node]) {
				if h := findActivity(m, 6); h != 0 {
					s.Host = h
					break
				}
			}
		case s.Kind == "fragment" || s.Kind == "dialog":
			var nav []int64
			for u := range rev[s.Node] {
				if b.nodes[u].Kind == "R" {
					nav = append(nav, u)
				}
			}
			for _, n := range order(nav) {
				if h := findActivity(n, 5); h != 0 {
					s.Host = h
					break
				}
			}
		}
	}
}

// ---------------------------------------------------------------- inputs

func isSource(p string) bool { return strings.HasSuffix(p, ".kt") || strings.HasSuffix(p, ".java") }

// watchedFiles: the sources, resources and manifests of the modules, relative to root.
func watchedFiles(root string, modules []Module) []string {
	var out []string
	for _, m := range modules {
		for i, d := range slices.Concat(m.Sources, m.Res) {
			srcDir := i < len(m.Sources)
			filepath.WalkDir(filepath.Join(root, d), func(p string, e os.DirEntry, err error) error {
				if err != nil || e.IsDir() {
					return nil
				}
				if !strings.HasPrefix(e.Name(), ".") && (isSource(e.Name()) || !srcDir) {
					rel, _ := filepath.Rel(root, p)
					out = append(out, rel)
				}
				return nil
			})
		}
		for _, rel := range m.Manifests {
			if isFile(filepath.Join(root, rel)) {
				out = append(out, rel)
			}
		}
	}
	return out
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(2)
}

// genericSupers: the supertypes with type arguments in a class Signature attribute, `Lp/S<Lp/A;>;`,
// those with type variables or wildcards left out (they match no field type exactly).
func genericSupers(sig string) []string {
	i := 0
	if strings.HasPrefix(sig, "<") { // the type parameters of the class
		for depth := 0; i < len(sig); i++ {
			if sig[i] == '<' {
				depth++
			} else if sig[i] == '>' {
				if depth--; depth == 0 {
					i++
					break
				}
			}
		}
	}
	var out []string
	for i < len(sig) {
		start, depth, exact := i, 0, true
		for ; i < len(sig) && (depth > 0 || sig[i] != ';'); i++ {
			switch sig[i] {
			case '<':
				depth++
			case '>':
				depth--
			case '*', '+', '-':
				exact = false
			}
			if depth > 0 && strings.IndexByte("<;[", sig[i]) >= 0 && i+1 < len(sig) && sig[i+1] == 'T' {
				exact = false // a type variable
			}
		}
		i++ // past the `;` of the type
		if t := sig[start:min(i, len(sig))]; exact && strings.Contains(t, "<") {
			out = append(out, t)
		}
	}
	return out
}
