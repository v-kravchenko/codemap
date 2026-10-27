package build

// Builds the code map from the compiled classes and the resources of the modules.

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
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

	"github.com/v-kravchenko/codemap/internal/android"
	"github.com/v-kravchenko/codemap/internal/classfile"
	"github.com/v-kravchenko/codemap/internal/config"
	"github.com/v-kravchenko/codemap/internal/project"
	"github.com/v-kravchenko/codemap/internal/snapshot"
	"github.com/v-kravchenko/codemap/internal/util"
)

var (
	routeNamePass = regexp.MustCompile(`^(kotlin/coroutines/jvm/internal/|kotlin/jvm/internal/Intrinsics\.` +
		`|kotlin/NoWhenBranchMatchedException\.)|\.access\$get`)
	mangled  = regexp.MustCompile(`-[\w]{7}$`)
	ktSuffix = regexp.MustCompile(`Kt$`)
)

var skipDirs = util.Set("build", "src", "node_modules")

func opensRoute(owner, name, caller string) bool {
	if config.RouteRegistration.MatchString(owner + "." + name) {
		return false
	}
	return !config.RouteReadOnly.MatchString(name) || name == "getRoute" && caller == config.StartDestination
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
	sum := sha256.Sum256([]byte(kind + "\x00" + owner + "\x00" + name + "\x00" + desc))
	v := int64(binary.BigEndian.Uint64(sum[:]) >> 1)
	if v == 0 {
		return 1
	}
	return v
}

// ---------------------------------------------------------------- modules

// templateModule: the module at rel with the paths of the source sets in the settings.
func templateModule(root, rel string, classes []string) project.Module {
	m := project.Module{Name: rel, Dir: rel, ClassDirs: classes}
	for _, s := range config.SourceSets {
		for _, lang := range []string{"kotlin", "java"} {
			m.Sources = append(m.Sources, filepath.Join(rel, "src", s, lang))
		}
		m.Manifests = append(m.Manifests, filepath.Join(rel, "src", s, "AndroidManifest.xml"))
	}
	for _, d := range android.ResDirs(filepath.Join(root, rel)) {
		r, _ := filepath.Rel(root, d)
		m.Res = append(m.Res, r)
	}
	return m
}

func DiscoverModules(root string) []project.Module {
	var out []project.Module
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
				isd = util.IsDir(filepath.Join(d, e.Name()))
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
		for _, c := range config.ClassDirs {
			p := filepath.Join(d, c)
			dirs = append(dirs, p)
			any = any || util.IsDir(p)
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
	C      *classfile.Class
}

type Builder struct {
	root    string
	modules []project.Module
	Jobs    int
	log     func(string)

	Nodes         map[int64]*Node
	Edges         map[snapshot.Edge]struct{}
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
	Screens       []*Screen
	resDefs       []ResDef
	related       map[[2]int64]struct{}
	content       map[[2]int64]struct{} // route, a composable its content calls from its own lines
	entries       map[EntryRow]struct{}
	callSites     []CallSite
	paramSwitches map[Triple]struct{}
	directReads   map[[2]int64]struct{}
	defaultGuards []Guarded
	Stats         map[string]string
	owned         map[int64][]int64
	Inputs        map[string]Stamp
	Sources       map[string]Stamp

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

func NewBuilder(root string, modules []project.Module, jobs int, log func(string)) *Builder {
	if jobs <= 0 {
		jobs = max(1, runtime.NumCPU()-1)
	}
	classfile.KeepStrings = config.StringRoutes
	return &Builder{root: root, modules: modules, Jobs: jobs, log: log,
		Nodes: make(map[int64]*Node, 1<<19), Edges: make(map[snapshot.Edge]struct{}, 1<<20), callVia: map[CallVia]struct{}{},
		classID: make(map[string]int64, 1<<16), memberID: make(map[[3]string]int64, 1<<19), resID: map[string]int64{},
		files: map[string]*File{}, related: map[[2]int64]struct{}{}, content: map[[2]int64]struct{}{}, entries: map[EntryRow]struct{}{},
		paramSwitches: map[Triple]struct{}{}, directReads: map[[2]int64]struct{}{}, Stats: map[string]string{},
		owned: make(map[int64][]int64, 1<<16), sourceNames: map[string]map[string][]string{}}
}

func (b *Builder) addNode(n *Node) int64 {
	owner := ""
	if n.Owner != 0 {
		owner = b.Nodes[n.Owner].Name
	}
	return b.addNodeID(nodeID(n.Kind, owner, n.Name, n.Desc), n)
}

func (b *Builder) addNodeID(id int64, n *Node) int64 {
	if old, ok := b.Nodes[id]; ok {
		util.Fail("code map: node id collision: %v and %v", [3]string{old.Kind, old.Name, old.Desc}, [3]string{n.Kind, n.Name, n.Desc})
	}
	b.Nodes[id] = n
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

func (b *Builder) sourceOf(m *project.Module, pkg, source string) string {
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

func (b *Builder) sourcesNamed(m *project.Module) map[string][]string {
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
	cls    *classfile.Class
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
			if !util.IsDir(cdir) {
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
	parallel(len(files), b.Jobs, func(i int) {
		r := &files[i]
		st, data, err := readStamp(r.path)
		if err != nil {
			r.err = err
			return
		}
		r.stamp = st
		r.cls, r.err = classfile.Parse(data)
	})
	b.Inputs = map[string]Stamp{}
	b.parsed = map[string]Parsed{}
	errors := 0
	for _, r := range files {
		if r.err != nil {
			errors++
			continue
		}
		rel, _ := filepath.Rel(b.root, r.path)
		b.Inputs[rel] = r.stamp
		b.parsed[r.cls.Name] = Parsed{r.module, r.cls}
	}
	b.parsedNames = make([]string, 0, len(b.parsed))
	for n := range b.parsed {
		b.parsedNames = append(b.parsedNames, n)
	}
	sort.Strings(b.parsedNames)
	watched := WatchedFiles(b.root, b.modules)
	stamps := make([]Stamp, len(watched))
	parallel(len(watched), b.Jobs, func(i int) { stamps[i], _, _ = readStamp(filepath.Join(b.root, watched[i])) })
	b.Sources = map[string]Stamp{}
	for i, w := range watched {
		b.Sources[w] = stamps[i]
	}
	b.Stats["source_files"] = fmt.Sprint(len(b.Sources))
	b.Stats["class_files"] = fmt.Sprint(len(files))
	b.Stats["parse_errors"] = fmt.Sprint(errors)
	b.Stats["parse_s"] = fmt.Sprintf("%.1f", time.Since(t0).Seconds())
	b.Timed("parse", t0)
	t := time.Now()
	b.classes()
	b.members()
	b.Timed("classes+members", t)
	t = time.Now()
	b.edgesAll()
	b.Timed("edges", t)
	t = time.Now()
	b.resources()
	b.Timed("resources", t)
	t = time.Now()
	b.screensAll()
	b.Timed("screens", t)
	b.Stats["build_s"] = fmt.Sprintf("%.1f", time.Since(t0).Seconds())
}

var verbose = os.Getenv("CODEMAP_TIMES") != ""

func (b *Builder) Timed(stage string, t time.Time) {
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
	parallel(len(keys), b.Jobs, func(i int) {
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
		id := b.addNode(&Node{Kind: "C", Name: name, Flags: int(p.C.Flags), File: fid, Module: p.Module, Sname: util.Dotted(shortName(name))})
		b.classID[name] = id
		b.classOrder = append(b.classOrder, name)
	}
	for _, name := range b.classOrder {
		cid := b.classID[name]
		n := b.Nodes[cid]
		top, _, _ := strings.Cut(name, "$")
		if t, ok := b.classID[top]; ok {
			n.Top = t
		} else {
			n.Top = cid
		}
		if n.File == 0 {
			if tn := b.Nodes[n.Top]; tn.File != 0 {
				n.File = tn.File
			}
		}
	}
	for _, name := range b.parsedNames {
		c := b.parsed[name].C
		cid := b.classID[name]
		if c.Super != "" {
			b.supers = append(b.supers, Super{cid, c.Super, snapshot.EXTENDS})
		}
		for _, i := range c.Interfaces {
			b.supers = append(b.supers, Super{cid, i, snapshot.IMPLEMENTS})
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
	parallel(len(b.parsedNames), b.Jobs, func(i int) {
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
		cn := b.Nodes[cid]
		nLines, known := fileLines[cn.File]
		if known && nLines < 0 {
			known = false
		}
		for _, f := range p.C.Fields {
			flags := int(f.Flags)
			if f.Constant {
				flags |= snapshot.ConstantFlag
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
	edges         []snapshot.Edge
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
				if r.Kind == classfile.RTableSet {
					ts = append(ts, tableSet{[3]string{r.Owner, r.Name, r.Desc}, r.Index})
				}
			}
		}
		b.classTables[n] = ts
	}
	b.routeClasses = b.subclassesOf(config.RouteBases)
	b.childClasses = b.subclassesOf(config.ChildBases)
	b.navTypes = map[string]bool{}
	for _, m := range []map[string]bool{b.routeClasses, b.childClasses, config.RouteInterfaces} {
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
		if b.parsed[n].C.Flags&snapshot.AccEnum != 0 {
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
	chunks := b.Jobs * 4
	size := max(1, (len(b.parsedNames)+chunks-1)/chunks)
	nChunks := (len(b.parsedNames) + size - 1) / size
	parallel(nChunks, b.Jobs, func(ci int) {
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
		b.Edges[snapshot.Edge{Src: e.Src, Dst: fix(e.Dst), Kind: e.Kind}] = struct{}{}
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

// classEdges gathers the edges of one class and what its code tells about routes.
type classEdges struct {
	*edgeWorker
	name          string
	info          *classfile.Class
	out           *part
	edges         oset[snapshot.Edge]
	callVia       oset[CallVia]
	paramSwitches oset[Triple]
	directReads   oset[[2]int64]
	routeOpens    oset[routeOpen]
	classLiterals oset[[2]int64]
	resKeys       map[string]int64
	resOrder      []string
	smap          []string // the classes of the SMAP within the map: whose functions were inlined here
	siteNo        int
	// the method being read
	mid        int64
	hasGuards  bool
	edgeGuards map[[2]int64]map[pb]bool // nil value: unguarded code
	egOrder    [][2]int64
	guard      map[pb]bool
}

// methodEdges is the state of reading one method's references in order.
type methodEdges struct {
	*classEdges
	m           *classfile.Method
	initializer bool
	pending     []content // edges of an initializer given to the field it is written to
	route       string
	cont        []content
	routeReads  []string
	named       []string
	strs        []string
	reified     bool // the next class literal is a type parameter of an inline function's own body
}

func (w *edgeWorker) edgesOf(name string) *part {
	info := w.b.parsed[name].C
	c := &classEdges{edgeWorker: w, name: name, info: info, out: &part{}, resKeys: map[string]int64{}}
	c.supers()
	c.fields()
	for _, s := range info.Smap {
		if _, ok := w.b.classID[s]; ok {
			c.smap = append(c.smap, s)
		}
	}
	for mi := range info.Methods {
		c.method(&info.Methods[mi])
	}
	out := c.out
	out.edges = c.edges.keys
	out.callVia = c.callVia.keys
	out.paramSwitches = c.paramSwitches.keys
	out.directReads = c.directReads.keys
	out.routeOpens = c.routeOpens.keys
	out.classLiterals = c.classLiterals.keys
	out.resKeys = c.resOrder
	return out
}

func (c *classEdges) add(e snapshot.Edge) {
	c.edges.add(e)
	if !c.hasGuards || e.Src != c.mid {
		return
	}
	k := [2]int64{e.Dst, int64(e.Kind)}
	old, in := c.edgeGuards[k]
	if !in {
		c.egOrder = append(c.egOrder, k)
	}
	if c.guard == nil || (in && old == nil) {
		c.edgeGuards[k] = nil
		return
	}
	m := map[pb]bool{}
	for x := range old {
		m[x] = true
	}
	for x := range c.guard {
		m[x] = true
	}
	c.edgeGuards[k] = m
}

// resRef: a placeholder id of a resource, made a node in mergeEdges.
func (c *classEdges) resRef(key string) int64 {
	if id, ok := c.resKeys[key]; ok {
		return id
	}
	id := -int64(len(c.resKeys)) - 1
	c.resKeys[key] = id
	c.resOrder = append(c.resOrder, key)
	return id
}

func (c *classEdges) supers() {
	b, cid := c.b, c.b.classID[c.name]
	if id, ok := b.classID[c.info.Super]; ok {
		c.add(snapshot.Edge{Src: cid, Dst: id, Kind: snapshot.EXTENDS})
	}
	for _, i := range c.info.Interfaces {
		if id, ok := b.classID[i]; ok {
			c.add(snapshot.Edge{Src: cid, Dst: id, Kind: snapshot.IMPLEMENTS})
		}
	}
}

func (c *classEdges) fields() {
	b := c.b
	for _, f := range c.info.Fields {
		fid := b.memberID[[3]string{c.name, f.Name, f.Desc}]
		for _, t := range descriptorClasses(f.Desc) {
			if id, ok := b.classID[t]; ok && t != c.name {
				c.add(snapshot.Edge{Src: fid, Dst: id, Kind: snapshot.TYPE})
			}
		}
		if f.Signature != "" {
			for _, t := range b.genericImpls[strings.ReplaceAll(f.Signature, "+L", "L")] {
				if id, ok := b.classID[t]; ok && t != c.name {
					c.add(snapshot.Edge{Src: fid, Dst: id, Kind: snapshot.INJECT})
				}
			}
		}
	}
}

func (c *classEdges) method(m *classfile.Method) {
	b, name := c.b, c.name
	c.mid = b.memberID[[3]string{name, m.Name, m.Desc}]
	c.hasGuards = len(m.Guards) > 0
	c.edgeGuards, c.egOrder, c.guard = nil, nil, nil
	if c.hasGuards {
		c.edgeGuards = map[[2]int64]map[pb]bool{}
	}
	for _, t := range descriptorClasses(m.Desc) {
		if id, ok := b.classID[t]; ok && t != name {
			c.add(snapshot.Edge{Src: c.mid, Dst: id, Kind: snapshot.TYPE})
		}
	}
	s := &methodEdges{classEdges: c, m: m, initializer: m.Name == "<init>" || m.Name == "<clinit>"}
	generated := strings.HasSuffix(name, "$WhenMappings") && m.Name == "<clinit>"
	for i := range m.Refs {
		ref := &m.Refs[i]
		if c.hasGuards {
			c.guard = nil
			if gs, ok := m.Guards[i]; ok {
				c.guard = map[pb]bool{}
				for _, g := range gs {
					c.guard[pb{g.Param, g.Bit}] = true
				}
			}
		}
		if generated || ref.Kind == classfile.RTableSet {
			continue
		}
		s.ref(ref)
	}
	s.finish()
}

func (s *methodEdges) ref(ref *classfile.Ref) {
	switch ref.Kind {
	case classfile.RString:
		s.strs = append(s.strs, ref.Owner)
	case classfile.RTableRead:
		s.tableRead(ref)
	case classfile.RType, classfile.RClassLit:
		s.typeRef(ref)
	case classfile.RLambda:
		s.lambda(ref)
	case classfile.RInline:
		for _, c := range s.smap {
			for _, target := range s.membersByName(c, ref.Name) {
				s.add(snapshot.Edge{Src: s.mid, Dst: target, Kind: snapshot.INLINE})
			}
		}
	default:
		s.member(ref)
	}
}

func (s *methodEdges) finish() {
	s.guard = nil
	for _, r := range s.routeReads {
		s.routeOpens.add(routeOpen{s.mid, r})
	}
	for _, pc := range s.pending {
		s.add(snapshot.Edge{Src: s.mid, Dst: pc.target, Kind: pc.kind})
		if pc.kind == snapshot.READ {
			s.directReads.add([2]int64{s.mid, pc.target})
		}
	}
	for _, k := range s.egOrder {
		for x := range s.edgeGuards[k] {
			s.out.defaultGuards = append(s.out.defaultGuards, Guarded{s.mid, k[0], int(k[1]), x.p, x.b})
		}
	}
}

// tableRead: a `when` over an enum reads the constants of its cases.
func (s *methodEdges) tableRead(ref *classfile.Ref) {
	tm := s.b.tables[[2]string{ref.Owner, ref.Name}]
	if tm == nil {
		return
	}
	for _, k := range tm.keys {
		index := tm.index[k]
		target := s.b.resolveMember(k[0], k[1], k[2], true)
		in := false
		for _, c := range ref.Cases {
			if int(c) == index {
				in = true
			}
		}
		if in && target != 0 {
			s.add(snapshot.Edge{Src: s.mid, Dst: target, Kind: snapshot.READ})
			if ref.Param >= 0 {
				s.paramSwitches.add(Triple{s.mid, target, int64(ref.Param)})
			}
		}
	}
}

func (s *methodEdges) typeRef(ref *classfile.Ref) {
	b, name, t := s.b, s.name, ref.Owner
	tid, ok := b.classID[t]
	if !ok || t == name {
		return
	}
	if ref.Kind == classfile.RClassLit && !strings.HasSuffix(s.m.Desc, ")Z") {
		s.classLiterals.add([2]int64{s.mid, tid})
	}
	if ref.Kind == classfile.RClassLit && s.reified {
		s.reified = false
	} else if (ref.Kind == classfile.RClassLit || ref.Index == 0xC1) && config.TypedRoutes && b.routeCandidate(t) {
		s.route = t // composable<T> or entry<T>, or `is T -> NavEntry(key)`
	}
	if s.initializer && strings.HasPrefix(t, name+"$") {
		s.pending = append(s.pending, content{tid, snapshot.TYPE})
	} else {
		s.add(snapshot.Edge{Src: s.mid, Dst: tid, Kind: snapshot.TYPE})
	}
	if strings.HasPrefix(t, name+"$") || inlinedLambdaOfSameFile(t, name) {
		s.cont = append(s.cont, content{tid, snapshot.TYPE})
	}
}

func (s *methodEdges) lambda(ref *classfile.Ref) {
	target, ok := s.b.memberID[[3]string{ref.Owner, ref.Name, ref.Desc}]
	if !ok {
		return
	}
	if s.initializer && ref.Owner == s.name {
		s.pending = append(s.pending, content{target, snapshot.LAMBDA})
	} else {
		s.add(snapshot.Edge{Src: s.mid, Dst: target, Kind: snapshot.LAMBDA})
	}
	s.cont = append(s.cont, content{target, snapshot.LAMBDA})
}

// member: a call, or a read or a write of a field.
func (s *methodEdges) member(ref *classfile.Ref) {
	b, name := s.b, s.name
	owner, n, d := ref.Owner, ref.Name, ref.Desc
	call := ref.Kind == classfile.RCall
	if call {
		s.routeCall(owner, n, d)
	}
	// the code of a route, of its nested classes (serializer) and of its sealed parent (whose
	// serializer lists the subclasses) makes the route without opening it
	typedOpen := config.TypedRoutes && owner != name && !strings.HasPrefix(name, owner+"$") && !b.ancestorOf(name, owner)
	if typedOpen && call && n == "<init>" && !(s.m.Name == "<init>" && owner == s.info.Super) && b.routeCandidate(owner) {
		s.routeReads = append(s.routeReads, owner)
	}
	if rtype := rClassType(owner); rtype != "" && ref.Kind == classfile.RRead {
		s.add(snapshot.Edge{Src: s.mid, Dst: s.resRef(android.ResKey(rtype, n)), Kind: snapshot.RES})
		return
	}
	if _, ok := b.classID[owner]; !ok {
		return
	}
	if ref.Kind == classfile.RRead && n == "INSTANCE" && b.routeClasses[owner] {
		s.route = owner
	}
	if ref.Kind == classfile.RRead && n == "INSTANCE" && (b.navTypes[owner] || typedOpen && b.routeCandidate(owner)) {
		s.routeReads = append(s.routeReads, owner)
	}
	target := b.resolveMember(owner, n, d, !call)
	if target == 0 {
		return
	}
	switch {
	case ref.Kind == classfile.RWrite:
		if len(s.pending) > 0 && owner == name {
			for _, pc := range s.pending {
				s.add(snapshot.Edge{Src: target, Dst: pc.target, Kind: pc.kind})
			}
			s.pending = nil
		}
		s.add(snapshot.Edge{Src: s.mid, Dst: target, Kind: snapshot.WRITE})
	case ref.Kind == classfile.RRead && s.initializer && owner != name && b.enumClasses[owner] && b.Nodes[target].Flags&snapshot.AccEnum != 0:
		s.pending = append(s.pending, content{target, snapshot.READ})
	case call:
		s.add(snapshot.Edge{Src: s.mid, Dst: target, Kind: snapshot.CALL})
		s.siteNo++
		s.siteArgs(&s.out.callSites, s.mid, target, d, ref.Args, s.siteNo, strings.HasSuffix(n, "$default"))
		if _, ok := b.memberID[[3]string{owner, n, d}]; !ok {
			s.callVia.add(CallVia{s.mid, target, owner})
		}
		if strings.HasPrefix(n, "getLambda$") && strings.HasPrefix(shortName(owner), "ComposableSingletons$") {
			s.cont = append(s.cont, content{target, snapshot.CALL})
		}
	default:
		s.add(snapshot.Edge{Src: s.mid, Dst: target, Kind: snapshot.READ})
		if ref.Kind == classfile.RRead && b.enumClasses[owner] && b.Nodes[target].Flags&snapshot.AccEnum != 0 {
			s.directReads.add([2]int64{s.mid, target})
		}
	}
}

// routeCall: what a call does to the routes: registers one, starts a graph, opens one by name or
// by the route objects handed to it.
func (s *methodEdges) routeCall(owner, n, d string) {
	full := owner + "." + n
	if n == "reifiedOperationMarker" && owner == "kotlin/jvm/internal/Intrinsics" {
		s.reified = true
	}
	var popped []string
	if len(s.strs) > 0 {
		if k := min(stringParams(d), len(s.strs)); k > 0 {
			popped = s.strs[len(s.strs)-k:]
			s.strs = s.strs[:len(s.strs)-k]
		}
	}
	if config.RouteGraph.MatchString(full) {
		if len(popped) >= 2 {
			s.out.childStart = append(s.out.childStart, [2]string{`"` + popped[1], `"` + popped[0]})
		} else if config.TypedRoutes && s.route != "" && len(s.routeReads) > 0 {
			s.out.childStart = append(s.out.childStart, [2]string{s.route, s.popRouteReads(1)[0]})
		}
		s.route = ""
	}
	if config.RouteRegistration.MatchString(full) {
		s.register(popped)
	}
	ownLambda := n == "<init>" && (strings.HasPrefix(owner, s.name+"$") || inlinedLambdaOfSameFile(owner, s.name))
	if len(s.cont) > 0 && strings.Contains(d, "Lkotlin/jvm/functions/Function") &&
		!config.RouteRegistration.MatchString(full) && !ownLambda {
		// lambdas handed to the app's own function (a graph section's onClick) are not the
		// content of the next destination, whatever names the routes; the content's own
		// object (an inlined `screen {}`) is made the same way
		if _, ok := s.b.classID[owner]; ok {
			s.cont = nil
		}
	}
	if len(s.named) > 0 && n != "getRoute" && !routeNamePass.MatchString(full) {
		if config.RouteByName.MatchString(full) {
			for _, r := range s.named {
				s.routeOpens.add(routeOpen{s.mid, r})
			}
		}
		s.named = nil
	}
	if len(popped) > 0 && config.RouteByName.MatchString(full) && opensRoute(owner, n, s.m.Name) {
		for _, p := range popped {
			s.routeOpens.add(routeOpen{s.mid, `"` + p})
		}
	}
	if len(s.routeReads) > 0 && config.TypedRoutes && (n == "areEqual" || n == "equals" ||
		owner == s.routeReads[len(s.routeReads)-1] && n == "serializer") {
		// compared, or asked for its serializer: not handed anywhere
		s.popRouteReads(1)
	}
	if len(s.routeReads) > 0 {
		s.handRouteReads(owner, n, d, full)
	}
}

// register: a call registering a route, named by the strings handed to it or by the route object
// read last, with the content gathered since the last one.
func (s *methodEdges) register(popped []string) {
	if s.route == "" && config.StringRoutes && len(popped) > 0 {
		s.route = `"` + popped[0]
	} else if s.route == "" && config.TypedRoutes && len(s.routeReads) > 0 {
		s.route = s.popRouteReads(1)[0]
	}
	if s.route != "" {
		s.out.registrations = append(s.out.registrations, registration{s.mid, s.route, s.cont})
	}
	s.route = ""
	s.cont = nil
}

// handRouteReads: the route objects read last, taken by a call by their types, start a child,
// open a route or name the route of getRoute.
func (s *methodEdges) handRouteReads(owner, n, d, full string) {
	b := s.b
	taken := 0
	if b.navTypes[owner] {
		taken++
	}
	if taken == 0 && config.TypedRoutes && config.RouteByName.MatchString(full) {
		taken = 1
	}
	for _, pd := range classfile.ParamDescs(d) {
		if c := classfile.ClassOfDesc(pd); c != "" && b.navTypes[c] {
			taken++
		}
	}
	if taken == 0 && (config.TypedRoutes || !config.RouteRegistration.MatchString(full)) {
		return
	}
	tk := s.popRouteReads(min(max(taken, 1), len(s.routeReads)))
	if n == "<init>" && config.ChildBases[owner] && b.childClasses[s.name] {
		s.out.childStart = append(s.out.childStart, [2]string{s.name, tk[len(tk)-1]})
	} else if opensRoute(owner, n, s.m.Name) {
		for _, r := range tk {
			s.routeOpens.add(routeOpen{s.mid, r})
		}
	} else if n == "getRoute" {
		s.named = append(s.named, tk...)
	}
}

// popRouteReads: the last k route objects read, taken off the stack.
func (s *methodEdges) popRouteReads(k int) []string {
	tk := append([]string(nil), s.routeReads[len(s.routeReads)-k:]...)
	s.routeReads = s.routeReads[:len(s.routeReads)-k]
	return tk
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
	for _, pd := range classfile.ParamDescs(desc) {
		if pd == "Ljava/lang/String;" {
			n++
		}
	}
	return n
}

func (w *edgeWorker) siteArgs(sites *[]CallSite, caller, callee int64, desc string, args []classfile.Arg, site int, defaults bool) {
	b := w.b
	for pos, pd := range classfile.ParamDescs(desc) {
		var a classfile.Arg
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
		if !b.enumClasses[classfile.ClassOfDesc(pd)] {
			continue
		}
		kind, value := "u", int64(0)
		switch {
		case a.K == 'n':
			kind = "n"
		case a.K == 'p':
			kind, value = "p", int64(a.V)
		case a.K == 'c' && b.enumClasses[a.Owner]:
			if fid := b.resolveMember(a.Owner, a.N, a.D, true); fid != 0 && b.Nodes[fid].Flags&snapshot.AccEnum != 0 {
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
			n := b.Nodes[v]
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
		byDotted[util.Dotted(name)] = name
	}
	suffixes := map[string][]string{}
	for _, name := range b.classOrder {
		d := util.Dotted(name)
		if byDotted[d] != name {
			continue // a later class took this dotted name: one entry per name
		}
		parts := strings.Split(d, ".")
		for i := 1; i < len(parts); i++ {
			k := strings.Join(parts[i:], ".")
			suffixes[k] = append(suffixes[k], name)
		}
	}
	n := 0
	scanned := make([][]*android.Resource, len(b.modules))
	parallel(len(b.modules), b.Jobs, func(i int) { scanned[i] = android.ScanModule(&b.modules[i], b.root) })
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
	parallel(len(resFiles), b.Jobs, func(i int) { counts[i] = lineCount(filepath.Join(b.root, resFiles[i])) })
	for i, f := range resFiles {
		lines[f] = counts[i]
	}
	b.lineCache = lines
	for i := range b.modules {
		for _, r := range scanned[i] {
			n++
			rid := b.resNode(r.Key)
			fid := b.fileID(r.File, i+1)
			node := b.Nodes[rid]
			if node.Module == 0 {
				node.Module, node.File = i+1, fid
			}
			b.resDefs = append(b.resDefs, ResDef{rid, fid, r.Line})
			for _, k := range util.SortedKeys(r.Refs) {
				if k != r.Key {
					b.Edges[snapshot.Edge{Src: rid, Dst: b.resNode(k), Kind: snapshot.XML_RES}] = struct{}{}
				}
			}
			for _, c := range util.SortedKeys(r.Classes) {
				name, ok := byDotted[c]
				if !ok {
					cands := suffixes[strings.TrimLeft(c, ".")]
					if len(cands) == 1 {
						name, ok = cands[0], true
					}
				}
				if ok {
					b.Edges[snapshot.Edge{Src: rid, Dst: b.classID[name], Kind: snapshot.XML_CLASS}] = struct{}{}
				}
			}
		}
	}
	b.Stats["resources"] = fmt.Sprint(n)
}

// -- screens

func (b *Builder) screensAll() {
	activities := b.subclassesOf(config.ActivityBases)
	fragments := b.subclassesOf(config.FragmentBases)
	dialogs := b.subclassesOf(config.DialogBases)
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
		if int(info.Flags)&(snapshot.AccAbstract|snapshot.AccInterface) != 0 || strings.HasPrefix(short, "Hilt_") || strings.Contains(name, "$") {
			continue
		}
		kind := "fragment"
		if activities[name] {
			kind = "activity"
		} else if dialogs[name] {
			kind = "dialog"
		}
		b.screenCls[b.classID[name]] = kind
		b.Screens = append(b.Screens, &Screen{b.classID[name], kind, util.Dotted(short), util.Dotted(name), 0})
	}
	previewAnn := map[string]bool{}
	for _, a := range b.annots {
		if config.PreviewMarkers[shortName(a.Name)] {
			previewAnn[a.Name] = true
		}
	}
	for _, a := range b.annots {
		if previewAnn[a.Name] && b.Nodes[a.Node].Kind == "M" {
			m := b.Nodes[a.Node]
			cls := b.Nodes[m.Owner].Name
			fn := normalizedName(m.Name)
			short, _, _ := strings.Cut(shortName(cls), "$")
			top, _, _ := strings.Cut(cls, "$")
			b.Screens = append(b.Screens, &Screen{a.Node, "preview", ktSuffix.ReplaceAllString(short, "") + "." + fn,
				strings.ReplaceAll(top, "/", ".") + "." + fn, 0})
		}
	}
	for _, key := range b.resOrder {
		rid := b.resID[key]
		if config.ScreenLayout.MatchString(key) && b.Nodes[rid].File != 0 {
			_, l, _ := strings.Cut(key, "/")
			b.Screens = append(b.Screens, &Screen{rid, "layout", l, l, 0})
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
				cn = b.Nodes[b.Nodes[r.method].Top] // the class registering it
			} else {
				label, qualified = util.Dotted(shortName(r.route)), util.Dotted(r.route)
				cn = b.Nodes[b.classID[r.route]]
			}
			rnode = b.addNode(&Node{Kind: "ROUTE", Name: r.route, Module: cn.Module, File: cn.File, Sname: label})
			routeNodes[r.route] = rnode
			b.Screens = append(b.Screens, &Screen{rnode, "route", label, qualified, 0})
		}
		for _, c := range r.content {
			delete(b.Edges, snapshot.Edge{Src: r.method, Dst: c.target, Kind: c.kind})
			b.Edges[snapshot.Edge{Src: rnode, Dst: c.target, Kind: snapshot.ROUTE_CONTENT}] = struct{}{}
			if c.kind == snapshot.TYPE {
				for _, init := range b.inits[c.target] {
					delete(b.Edges, snapshot.Edge{Src: r.method, Dst: init, Kind: snapshot.CALL})
				}
			}
		}
		b.Edges[snapshot.Edge{Src: rnode, Dst: r.method, Kind: snapshot.ROUTE_HOST}] = struct{}{}
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
					b.Edges[snapshot.Edge{Src: ro.method, Dst: rn, Kind: snapshot.OPENS}] = struct{}{}
				}
			}
			continue
		}
		if s, ok := b.childStart[rc]; ok {
			rc = s
		}
		if rn, ok := routeNodes[rc]; ok {
			b.Edges[snapshot.Edge{Src: ro.method, Dst: rn, Kind: snapshot.OPENS}] = struct{}{}
		}
	}
	for cl := range b.classLiterals {
		if b.screenCls[cl[1]] == "activity" {
			b.Edges[snapshot.Edge{Src: cl[0], Dst: cl[1], Kind: snapshot.OPENS}] = struct{}{}
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
	g         *android.NavGraph
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
			for _, sub := range util.ListDir(rdir) {
				if android.ResourceTypeOfDir(sub) != "navigation" {
					continue
				}
				for _, f := range util.ListDir(filepath.Join(rdir, sub)) {
					if !strings.HasSuffix(f, ".xml") {
						continue
					}
					data, err := os.ReadFile(filepath.Join(rdir, sub, f))
					if err != nil {
						continue
					}
					g := android.ParseNavigation(data)
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
	var resolve func(dest string, g *android.NavGraph, depth int) map[string]bool
	resolve = func(dest string, g *android.NavGraph, depth int) map[string]bool {
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
		for _, cls := range util.SortedKeys(resolve(e.dest, e.g, 0)) {
			cid, ok := b.classID[strings.ReplaceAll(cls, ".", "/")]
			if hasR && ok {
				if _, scr := b.screenCls[cid]; scr {
					b.Edges[snapshot.Edge{Src: rid, Dst: cid, Kind: snapshot.OPENS}] = struct{}{}
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
			if !util.IsFile(path) {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			for _, e := range android.ParseManifest(string(data)) {
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
	outEdges := map[int64][]snapshot.Edge{}
	for e := range b.Edges {
		outEdges[e.Src] = append(outEdges[e.Src], e)
	}
	previewsByFile := map[int][]int64{}
	layouts := map[int64]bool{}
	for _, s := range b.Screens {
		switch s.Kind {
		case "preview":
			f := b.Nodes[s.Node].File
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
				if b.Nodes[m].Kind == "M" {
					out = append(out, m)
				}
			}
			i++
		}
		return out
	}
	isComposable := func(id int64) bool {
		return strings.Contains(b.Nodes[id].Desc, "Landroidx/compose/runtime/Composer;")
	}
	collect := func(screen int64, methods []int64, ownFile int) {
		module := b.Nodes[screen].Module
		files := map[int]bool{}
		if ownFile != 0 {
			files[ownFile] = true
		}
		for _, m := range methods {
			for _, e := range outEdges[m] {
				d, k := e.Dst, e.Kind
				dn := b.Nodes[d]
				if (k == snapshot.CALL || k == snapshot.LAMBDA) && dn.Kind == "M" && isComposable(d) && dn.Module == module {
					files[dn.File] = true
				} else if k == snapshot.RES && layouts[d] {
					b.related[[2]int64{screen, d}] = struct{}{}
				} else if (k == snapshot.TYPE || k == snapshot.CALL) && (dn.Kind == "C" || dn.Kind == "M") {
					cn := d
					if dn.Kind == "M" {
						cn = dn.Owner
					}
					cname := b.Nodes[cn].Name
					if strings.HasSuffix(cname, "Binding") && strings.Contains(cname, "/databinding/") {
						for _, bm := range nested(cname) {
							for _, e2 := range outEdges[bm] {
								if e2.Kind == snapshot.RES && layouts[e2.Dst] {
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
	for _, s := range b.Screens {
		switch s.Kind {
		case "activity", "fragment", "dialog":
			collect(s.Node, nested(b.Nodes[s.Node].Name), b.Nodes[s.Node].File)
		case "route":
			var methods []int64
			from := map[int]bool{} // the files registering the route
			add := func(d int64) {
				if dn := b.Nodes[d]; dn.Kind == "C" {
					methods = append(methods, nested(dn.Name)...)
				} else {
					methods = append(methods, d)
				}
			}
			for _, e := range outEdges[s.Node] {
				switch e.Kind {
				case snapshot.ROUTE_HOST:
					from[b.Nodes[e.Dst].File] = true
				case snapshot.ROUTE_CONTENT:
					add(e.Dst)
					// a ComposableSingletons getter: the lambda its field holds
					for _, r := range outEdges[e.Dst] {
						if r.Kind == snapshot.READ && b.Nodes[r.Dst].Owner == b.Nodes[e.Dst].Owner {
							for _, l := range outEdges[r.Dst] {
								if l.Kind == snapshot.LAMBDA || l.Kind == snapshot.TYPE {
									add(l.Dst)
								}
							}
						}
					}
				}
			}
			collect(s.Node, methods, 0)
			for _, c := range b.ownComposableCalls(methods, from) {
				b.content[[2]int64{s.Node, c}] = struct{}{}
			}
		}
	}
}

// ownComposableCalls: the repo composables `methods` call from code written where the route is
// registered (in a file of `from`): the content it draws (`{ ConfirmScreen(vm) }`), not what an
// inlined `screen<VM> {}` adds around it. In a copy of a wrapper's class SMAP gives the wrapper's
// lines to the class copied, the rest to where they were written.
func (b *Builder) ownComposableCalls(methods []int64, from map[int]bool) []int64 {
	made := func(cls string) bool { p, ok := b.parsed[cls]; return ok && p.C.Enclosing[0] != "" }
	var out []int64
	for _, mid := range methods {
		mn := b.Nodes[mid]
		cls := b.parsed[b.Nodes[mn.Owner].Name].C
		for _, m := range cls.Methods {
			if m.Name != mn.Name || m.Desc != mn.Desc {
				continue
			}
			for _, r := range m.Refs {
				if r.Kind != classfile.RCall || !strings.Contains(r.Desc, "Landroidx/compose/runtime/Composer;") {
					continue
				}
				if src := cls.SourceOf(r.Line); src != cls.Name {
					if id, ok := b.classID[src]; !ok || src == cls.SmapOrigin || !from[b.Nodes[id].File] {
						continue
					}
				}
				t := b.resolveMember(r.Owner, r.Name, r.Desc, false)
				if t == 0 || made(b.Nodes[b.Nodes[t].Owner].Name) || strings.Contains(b.Nodes[t].Name, "lambda$") || strings.Contains(b.Nodes[t].Name, "$lambda") {
					continue // a lambda calling itself again on recomposition
				}
				out = append(out, t)
			}
		}
	}
	return out
}

func (b *Builder) hosts() {
	rev := map[int64]map[int64]bool{}
	routeHost := map[int64][]int64{}
	for e := range b.Edges {
		switch e.Kind {
		case snapshot.CALL, snapshot.RES, snapshot.XML_RES, snapshot.XML_CLASS, snapshot.TYPE:
			if rev[e.Dst] == nil {
				rev[e.Dst] = map[int64]bool{}
			}
			rev[e.Dst][e.Src] = true
		case snapshot.ROUTE_HOST:
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
		n := b.Nodes[i]
		o := ""
		if n.Owner != 0 {
			o = b.Nodes[n.Owner].Name
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
				top := b.Nodes[n].Top
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
	for _, s := range b.Screens {
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
				if b.Nodes[u].Kind == "R" {
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

func IsSource(p string) bool { return strings.HasSuffix(p, ".kt") || strings.HasSuffix(p, ".java") }

// watchedFiles: the sources, resources and manifests of the modules, relative to root.
func WatchedFiles(root string, modules []project.Module) []string {
	var out []string
	for _, m := range modules {
		for i, d := range slices.Concat(m.Sources, m.Res) {
			srcDir := i < len(m.Sources)
			filepath.WalkDir(filepath.Join(root, d), func(p string, e os.DirEntry, err error) error {
				if err != nil || e.IsDir() {
					return nil
				}
				if !strings.HasPrefix(e.Name(), ".") && (IsSource(e.Name()) || !srcDir) {
					rel, _ := filepath.Rel(root, p)
					out = append(out, rel)
				}
				return nil
			})
		}
		for _, rel := range m.Manifests {
			if util.IsFile(filepath.Join(root, rel)) {
				out = append(out, rel)
			}
		}
	}
	return out
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

// Map: the built map in sorted columns.
func (b *Builder) Map(meta map[string]string) *snapshot.Map {
	m := &snapshot.Map{}
	for _, md := range b.modules {
		m.Module.Name.Add(md.Name)
		m.Module.Dir.Add(filepath.ToSlash(md.Dir)) // paths in the map use /, on Windows too: the same answers everywhere
	}
	for _, p := range b.fileOrder {
		f := b.files[p]
		m.File.ID = append(m.File.ID, int32(f.ID))
		m.File.Module = append(m.File.Module, int32(f.Module))
		m.File.Lines = append(m.File.Lines, int32(f.Lines))
		m.File.Path.Add(filepath.ToSlash(p))
	}

	ids := make([]int64, 0, len(b.Nodes))
	for id := range b.Nodes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var wg sync.WaitGroup
	wg.Go(func() { m.Out, m.In = snapshot.EdgeCSR(b.Edges, ids) })
	wg.Go(func() { b.otherColumns(m) })
	n := &m.Node
	k := len(ids)
	n.ID, n.Owner, n.Top = ids, make([]int64, k), make([]int64, k)
	n.Flags, n.File, n.Module, n.LMin, n.LMax = make([]int32, k), make([]int32, k), make([]int32, k), make([]int32, k), make([]int32, k)
	for _, s := range []*snapshot.Strs{&n.Kind, &n.Name, &n.Desc, &n.Sname} {
		s.Off = make([]uint32, 1, k+1)
	}
	n.Name.Blob, n.Desc.Blob, n.Sname.Blob = make([]byte, 0, 8<<20), make([]byte, 0, 16<<20), make([]byte, 0, 8<<20)
	for i, id := range ids {
		v := b.Nodes[id]
		n.Owner[i], n.Top[i] = v.Owner, v.Top
		n.Kind.Add(v.Kind)
		n.Name.Add(v.Name)
		n.Desc.Add(v.Desc)
		n.Sname.Add(v.Sname)
		n.Flags[i], n.File[i], n.Module[i], n.LMin[i], n.LMax[i] = int32(v.Flags), int32(v.File), int32(v.Module), int32(v.LMin), int32(v.LMax)
	}
	wg.Wait()
	m.Derive()

	mt := map[string]string{}
	for k, v := range meta {
		mt[k] = v
	}
	for k, v := range b.Stats {
		mt[k] = v
	}
	mt["nodes"], mt["edges"], mt["screens"] = fmt.Sprint(len(b.Nodes)), fmt.Sprint(len(b.Edges)), fmt.Sprint(len(b.Screens))
	for _, k := range util.SortedKeys(mt) {
		m.Meta.Key.Add(k)
		m.Meta.Value.Add(mt[k])
	}
	return m
}

// otherColumns: the columns of every table but node and edge.
func (b *Builder) otherColumns(m *snapshot.Map) {
	via := make([]CallVia, 0, len(b.callVia))
	for v := range b.callVia {
		via = append(via, v)
	}
	sort.Slice(via, func(i, j int) bool {
		a, c := via[i], via[j]
		if a.Src != c.Src {
			return a.Src < c.Src
		}
		if a.Dst != c.Dst {
			return a.Dst < c.Dst
		}
		return a.Via < c.Via
	})
	for _, v := range via {
		m.CallVia.Src = append(m.CallVia.Src, v.Src)
		m.CallVia.Dst = append(m.CallVia.Dst, v.Dst)
		m.CallVia.Via.Add(v.Via)
	}
	for _, s := range b.supers {
		m.Hierarchy.Cls = append(m.Hierarchy.Cls, s.Cls)
		m.Hierarchy.Sup.Add(s.Sup)
		m.Hierarchy.Kind = append(m.Hierarchy.Kind, int32(s.Kind))
	}
	for _, a := range b.annots {
		m.Annotation.Node = append(m.Annotation.Node, a.Node)
		m.Annotation.Name.Add(a.Name)
	}
	for _, r := range b.resDefs {
		m.ResDef.Node = append(m.ResDef.Node, r.Node)
		m.ResDef.File = append(m.ResDef.File, int32(r.File))
		m.ResDef.Line = append(m.ResDef.Line, int32(r.Line))
	}
	cs := &m.CallSite
	for _, c := range b.callSites {
		cs.Src = append(cs.Src, c.Src)
		cs.Dst = append(cs.Dst, c.Dst)
		cs.Arg = append(cs.Arg, c.Arg)
		cs.Site = append(cs.Site, int32(c.Site))
		cs.Pos = append(cs.Pos, int32(c.Pos))
		cs.Kind.Add(c.Kind)
	}
	ps := make([]Triple, 0, len(b.paramSwitches))
	for t := range b.paramSwitches {
		ps = append(ps, t)
	}
	sort.Slice(ps, func(i, j int) bool { return lessTriple(ps[i], ps[j]) })
	for _, t := range ps {
		m.ParamSwitch.Method = append(m.ParamSwitch.Method, t.A)
		m.ParamSwitch.Entry = append(m.ParamSwitch.Entry, t.B)
		m.ParamSwitch.Pos = append(m.ParamSwitch.Pos, t.C)
	}
	for _, p := range snapshot.SortedPairs(b.directReads) {
		m.DirectRead.Method = append(m.DirectRead.Method, p[0])
		m.DirectRead.Entry = append(m.DirectRead.Entry, p[1])
	}
	dg := append([]Guarded(nil), b.defaultGuards...)
	sort.Slice(dg, func(i, j int) bool {
		a, c := dg[i], dg[j]
		if a.Method != c.Method {
			return a.Method < c.Method
		}
		if a.Dst != c.Dst {
			return a.Dst < c.Dst
		}
		if a.Kind != c.Kind {
			return a.Kind < c.Kind
		}
		if a.Pos != c.Pos {
			return a.Pos < c.Pos
		}
		return a.Bit < c.Bit
	})
	g := &m.DefaultGuard
	for _, d := range dg {
		g.Method = append(g.Method, d.Method)
		g.Dst = append(g.Dst, d.Dst)
		g.Kind = append(g.Kind, int32(d.Kind))
		g.Pos = append(g.Pos, int32(d.Pos))
		g.Bit = append(g.Bit, int32(d.Bit))
	}
	all := map[string]Stamp{}
	for k, v := range b.Inputs {
		all[k] = v
	}
	for k, v := range b.Sources {
		all[k] = v
	}
	for _, p := range util.SortedKeys(all) {
		s := all[p]
		m.Input.Path.Add(filepath.ToSlash(p))
		m.Input.Sha1.Add(s.Sha1)
		m.Input.Size = append(m.Input.Size, s.Size)
		m.Input.Mtime = append(m.Input.Mtime, s.Mtime)
	}
	for _, s := range b.Screens {
		m.Screen.Node = append(m.Screen.Node, s.Node)
		m.Screen.Host = append(m.Screen.Host, s.Host)
		m.Screen.Kind.Add(s.Kind)
		m.Screen.Label.Add(s.Label)
		m.Screen.Target.Add(s.Target)
	}
	for _, p := range snapshot.SortedPairs(b.related) {
		m.ScreenRelated.Screen = append(m.ScreenRelated.Screen, p[0])
		m.ScreenRelated.Related = append(m.ScreenRelated.Related, p[1])
	}
	for _, p := range snapshot.SortedPairs(b.content) {
		m.ScreenContent.Screen = append(m.ScreenContent.Screen, p[0])
		m.ScreenContent.Node = append(m.ScreenContent.Node, p[1])
	}
	ent := make([]EntryRow, 0, len(b.entries))
	for e := range b.entries {
		ent = append(ent, e)
	}
	sort.Slice(ent, func(i, j int) bool {
		a, c := ent[i], ent[j]
		if a.Node != c.Node {
			return a.Node < c.Node
		}
		if a.Kind != c.Kind {
			return a.Kind < c.Kind
		}
		if a.Detail != c.Detail {
			return a.Detail < c.Detail
		}
		if a.File != c.File {
			return a.File < c.File
		}
		return a.Line < c.Line
	})
	for _, e := range ent {
		m.Entry.Node = append(m.Entry.Node, e.Node)
		m.Entry.Kind.Add(e.Kind)
		m.Entry.Detail.Add(e.Detail)
		m.Entry.File = append(m.Entry.File, int32(e.File))
		m.Entry.Line = append(m.Entry.Line, int32(e.Line))
	}
}

func lessTriple(a, c Triple) bool {
	if a.A != c.A {
		return a.A < c.A
	}
	if a.B != c.B {
		return a.B < c.B
	}
	return a.C < c.C
}
