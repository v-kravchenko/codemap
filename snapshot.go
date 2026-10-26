package main

// The map as flat sorted columns, and their binary snapshot: what the CLI loads instead of SQLite.
//
// Snapshot: magic, format version, column count (u32 each), then each column as a u64 byte length
// and its bytes padded to 8. Numbers are little-endian (every target: amd64, arm64). Loading maps
// the columns onto the file bytes without copies.

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"unsafe"
)

const (
	snapMagic   = "CODEMAP\x00"
	snapVersion = 3
)

// Strs: strings of a column, s[i] = blob[off[i]:off[i+1]].
// ponytail: no dedup, ~41 of 75 MB are repeated descs/paths; a shared string pool if load time matters.
type Strs struct {
	off  []uint32
	blob []byte
}

func (s *Strs) Add(v string) {
	if s.off == nil {
		s.off = []uint32{0}
	}
	s.blob = append(s.blob, v...)
	s.off = append(s.off, uint32(len(s.blob)))
}

func (s *Strs) At(i int) string {
	b := s.blob[s.off[i]:s.off[i+1]]
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func (s *Strs) Len() int { return max(len(s.off)-1, 0) }

// CSR: edges of node i are Node[Start[i]:Start[i+1]] (node indexes), with their Kind.
type CSR struct{ Start, Node, Kind []int32 }

// Map: the tables of the SQLite map; ids are node ids, node rows are sorted by id. Edges are kept
// as CSR over node indexes: Out (by src, then dst, kind) and In (by dst, then src, kind).
type Map struct {
	Meta   struct{ Key, Value Strs }
	Module struct{ Name, Dir Strs } // id = index + 1
	File   struct {
		ID, Module, Lines []int32 // Lines -1: unreadable
		Path              Strs
	}
	Node struct {
		ID, Owner, Top                  []int64 // 0: none
		Kind, Name, Desc, Sname         Strs
		Flags, File, Module, LMin, LMax []int32
		OwnerIx, TopIx                  []int32 // node indexes, -1: none
	}
	Out, In    CSR
	OpenedBy   OpenedRows // by screen, then src (precomputeOpenedBy)
	OpenedBase BaseRows
	Members    CSR // by owner, in id order
	Overriders CSR // method -> the classes below its class that declare it again (derive)
	CallVia    struct {
		Src, Dst []int64
		Via      Strs
	}
	Hierarchy struct {
		ClsIx, SupIx []int32 // SupIx -1: not a repo class
		Cls          []int64
		Sup          Strs
		Kind         []int32
	}
	Annotation struct {
		Node []int64
		Name Strs
	}
	ResDef struct {
		Node       []int64
		File, Line []int32
	}
	CallSite struct {
		Src, Dst, Arg []int64
		Site, Pos     []int32
		Kind          Strs
	}
	ParamSwitch  struct{ Method, Entry, Pos []int64 }
	DirectRead   struct{ Method, Entry []int64 }
	DefaultGuard struct {
		Method, Dst    []int64
		Kind, Pos, Bit []int32
	}
	Input struct {
		Path, Sha1  Strs
		Size, Mtime []int64
	}
	Screen struct {
		Node, Host          []int64
		Kind, Label, Target Strs
	}
	ScreenRelated struct{ Screen, Related []int64 }
	Entry         struct {
		Node         []int64
		Kind, Detail Strs
		File, Line   []int32
	}
}

// OpenedRows: the ways each screen is opened (graph.py's opened_by table); Hub, HostNode -1: none.
type OpenedRows struct {
	Screen, Src, Hub, HostNode []int32
	HostModule                 Strs
	Via                        CSR
}

// BaseRows: base classes that open a screen, with the screens extending them (opened_base).
type BaseRows struct {
	Screen, Base []int32
	Below        CSR
}

type codec interface {
	i64(*[]int64)
	i32(*[]int32)
	strs(*Strs)
}

// columns lists every column in snapshot order; writing and reading both walk it.
func (m *Map) columns(c codec) {
	c.strs(&m.Meta.Key)
	c.strs(&m.Meta.Value)
	c.strs(&m.Module.Name)
	c.strs(&m.Module.Dir)
	f := &m.File
	c.i32(&f.ID)
	c.i32(&f.Module)
	c.i32(&f.Lines)
	c.strs(&f.Path)
	n := &m.Node
	c.i64(&n.ID)
	c.i64(&n.Owner)
	c.i64(&n.Top)
	c.strs(&n.Kind)
	c.strs(&n.Name)
	c.strs(&n.Desc)
	c.strs(&n.Sname)
	c.i32(&n.Flags)
	c.i32(&n.File)
	c.i32(&n.Module)
	c.i32(&n.LMin)
	c.i32(&n.LMax)
	c.i32(&n.OwnerIx)
	c.i32(&n.TopIx)
	for _, g := range []*CSR{&m.Out, &m.In, &m.Members, &m.Overriders} {
		c.i32(&g.Start)
		c.i32(&g.Node)
		c.i32(&g.Kind)
	}
	c.i64(&m.CallVia.Src)
	c.i64(&m.CallVia.Dst)
	c.strs(&m.CallVia.Via)
	c.i64(&m.Hierarchy.Cls)
	c.strs(&m.Hierarchy.Sup)
	c.i32(&m.Hierarchy.Kind)
	c.i32(&m.Hierarchy.ClsIx)
	c.i32(&m.Hierarchy.SupIx)
	ob := &m.OpenedBy
	for _, col := range []*[]int32{&ob.Screen, &ob.Src, &ob.Hub, &ob.HostNode, &ob.Via.Start, &ob.Via.Node} {
		c.i32(col)
	}
	c.strs(&ob.HostModule)
	b := &m.OpenedBase
	for _, col := range []*[]int32{&b.Screen, &b.Base, &b.Below.Start, &b.Below.Node} {
		c.i32(col)
	}
	c.i64(&m.Annotation.Node)
	c.strs(&m.Annotation.Name)
	c.i64(&m.ResDef.Node)
	c.i32(&m.ResDef.File)
	c.i32(&m.ResDef.Line)
	cs := &m.CallSite
	c.i64(&cs.Src)
	c.i64(&cs.Dst)
	c.i64(&cs.Arg)
	c.i32(&cs.Site)
	c.i32(&cs.Pos)
	c.strs(&cs.Kind)
	c.i64(&m.ParamSwitch.Method)
	c.i64(&m.ParamSwitch.Entry)
	c.i64(&m.ParamSwitch.Pos)
	c.i64(&m.DirectRead.Method)
	c.i64(&m.DirectRead.Entry)
	g := &m.DefaultGuard
	c.i64(&g.Method)
	c.i64(&g.Dst)
	c.i32(&g.Kind)
	c.i32(&g.Pos)
	c.i32(&g.Bit)
	c.strs(&m.Input.Path)
	c.strs(&m.Input.Sha1)
	c.i64(&m.Input.Size)
	c.i64(&m.Input.Mtime)
	s := &m.Screen
	c.i64(&s.Node)
	c.i64(&s.Host)
	c.strs(&s.Kind)
	c.strs(&s.Label)
	c.strs(&s.Target)
	c.i64(&m.ScreenRelated.Screen)
	c.i64(&m.ScreenRelated.Related)
	e := &m.Entry
	c.i64(&e.Node)
	c.strs(&e.Kind)
	c.strs(&e.Detail)
	c.i32(&e.File)
	c.i32(&e.Line)
}

func (m *Map) Nodes() int { return len(m.Node.ID) }
func (m *Map) Edges() int { return len(m.Out.Node) }

func (m *Map) MetaValue(key string) string {
	for i := range m.Meta.Key.Len() {
		if m.Meta.Key.At(i) == key {
			return m.Meta.Value.At(i)
		}
	}
	return ""
}

// Map: the built map in sorted columns, in the row order of the Python map.
func (b *Builder) Map(meta map[string]string) *Map {
	m := &Map{}
	for _, md := range b.modules {
		m.Module.Name.Add(md.Name)
		m.Module.Dir.Add(md.Dir)
	}
	for _, p := range b.fileOrder {
		f := b.files[p]
		m.File.ID = append(m.File.ID, int32(f.ID))
		m.File.Module = append(m.File.Module, int32(f.Module))
		m.File.Lines = append(m.File.Lines, int32(f.Lines))
		m.File.Path.Add(p)
	}

	ids := make([]int64, 0, len(b.nodes))
	for id := range b.nodes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var wg sync.WaitGroup
	wg.Go(func() { m.Out, m.In = edgeCSR(b.edges, ids) })
	wg.Go(func() { b.otherColumns(m) })
	n := &m.Node
	k := len(ids)
	n.ID, n.Owner, n.Top = ids, make([]int64, k), make([]int64, k)
	n.Flags, n.File, n.Module, n.LMin, n.LMax = make([]int32, k), make([]int32, k), make([]int32, k), make([]int32, k), make([]int32, k)
	for _, s := range []*Strs{&n.Kind, &n.Name, &n.Desc, &n.Sname} {
		s.off = make([]uint32, 1, k+1)
	}
	n.Name.blob, n.Desc.blob, n.Sname.blob = make([]byte, 0, 8<<20), make([]byte, 0, 16<<20), make([]byte, 0, 8<<20)
	for i, id := range ids {
		v := b.nodes[id]
		n.Owner[i], n.Top[i] = v.Owner, v.Top
		n.Kind.Add(v.Kind)
		n.Name.Add(v.Name)
		n.Desc.Add(v.Desc)
		n.Sname.Add(v.Sname)
		n.Flags[i], n.File[i], n.Module[i], n.LMin[i], n.LMax[i] = int32(v.Flags), int32(v.File), int32(v.Module), int32(v.LMin), int32(v.LMax)
	}
	wg.Wait()
	m.derive()

	mt := map[string]string{}
	for k, v := range meta {
		mt[k] = v
	}
	for k, v := range b.stats {
		mt[k] = v
	}
	mt["nodes"], mt["edges"], mt["screens"] = fmt.Sprint(len(b.nodes)), fmt.Sprint(len(b.edges)), fmt.Sprint(len(b.screens))
	for _, k := range sortedKeys(mt) {
		m.Meta.Key.Add(k)
		m.Meta.Value.Add(mt[k])
	}
	return m
}

// otherColumns: the columns of every table but node and edge.
func (b *Builder) otherColumns(m *Map) {
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
	for _, p := range sortedPairs(b.directReads) {
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
	for k, v := range b.inputs {
		all[k] = v
	}
	for k, v := range b.sources {
		all[k] = v
	}
	for _, p := range sortedKeys(all) {
		s := all[p]
		m.Input.Path.Add(p)
		m.Input.Sha1.Add(s.Sha1)
		m.Input.Size = append(m.Input.Size, s.Size)
		m.Input.Mtime = append(m.Input.Mtime, s.Mtime)
	}
	for _, s := range b.screens {
		m.Screen.Node = append(m.Screen.Node, s.Node)
		m.Screen.Host = append(m.Screen.Host, s.Host)
		m.Screen.Kind.Add(s.Kind)
		m.Screen.Label.Add(s.Label)
		m.Screen.Target.Add(s.Target)
	}
	for _, p := range sortedPairs(b.related) {
		m.ScreenRelated.Screen = append(m.ScreenRelated.Screen, p[0])
		m.ScreenRelated.Related = append(m.ScreenRelated.Related, p[1])
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

func sortedPairs(set map[[2]int64]struct{}) [][2]int64 {
	out := make([][2]int64, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i][0] < out[j][0] || out[i][0] == out[j][0] && out[i][1] < out[j][1]
	})
	return out
}

// edgeCSR sorts the edges by radix passes over node indexes (kind, dst, src: stable), no comparisons.
func edgeCSR(edges map[Edge]struct{}, ids []int64) (out, in CSR) {
	idx := make(map[int64]int32, len(ids))
	for i, id := range ids {
		idx[id] = int32(i)
	}
	m := len(edges)
	src, dst, kind := make([]int32, m), make([]int32, m), make([]int32, m)
	maxKind := 0
	i := 0
	for e := range edges {
		s, ok1 := idx[e.Src]
		d, ok2 := idx[e.Dst]
		if !ok1 || !ok2 || e.Kind < 0 {
			panic(fmt.Sprintf("edge %d -> %d (kind %d): no such node", e.Src, e.Dst, e.Kind))
		}
		src[i], dst[i], kind[i] = s, d, int32(e.Kind)
		maxKind = max(maxKind, e.Kind)
		i++
	}
	n := len(idx)
	order := make([]int32, m)
	for i := range order {
		order[i] = int32(i)
	}
	order, _ = countingSort(order, kind, maxKind+1)
	order, _ = countingSort(order, dst, n)
	order, outStart := countingSort(order, src, n)
	out = CSR{Start: outStart, Node: make([]int32, m), Kind: make([]int32, m)}
	for i, e := range order {
		out.Node[i], out.Kind[i] = dst[e], kind[e]
	}
	order, inStart := countingSort(order, dst, n)
	in = CSR{Start: inStart, Node: make([]int32, m), Kind: make([]int32, m)}
	for i, e := range order {
		in.Node[i], in.Kind[i] = src[e], kind[e]
	}
	return out, in
}

// countingSort orders the items stably by key[item] in [0, r); start[k] is where key k begins.
func countingSort(order, key []int32, r int) (sorted, start []int32) {
	start = make([]int32, r+1)
	for _, e := range order {
		start[key[e]+1]++
	}
	for k := 1; k <= r; k++ {
		start[k] += start[k-1]
	}
	pos := slices.Clone(start)
	sorted = make([]int32, len(order))
	for _, e := range order {
		sorted[pos[key[e]]] = e
		pos[key[e]]++
	}
	return sorted, start
}

type snapWriter struct {
	w     *bufio.Writer
	count uint32
}

var zeros [8]byte

func (w *snapWriter) raw(p unsafe.Pointer, size int) {
	w.w.Write(binary.LittleEndian.AppendUint64(nil, uint64(size)))
	if size > 0 {
		w.w.Write(unsafe.Slice((*byte)(p), size))
	}
	pad := (8 - size%8) % 8
	w.w.Write(zeros[:pad])
	w.count++
}

func (w *snapWriter) i64(s *[]int64) { w.raw(unsafe.Pointer(unsafe.SliceData(*s)), len(*s)*8) }
func (w *snapWriter) i32(s *[]int32) { w.raw(unsafe.Pointer(unsafe.SliceData(*s)), len(*s)*4) }
func (w *snapWriter) strs(s *Strs) {
	if s.off == nil {
		s.off = []uint32{0}
	}
	w.raw(unsafe.Pointer(unsafe.SliceData(s.off)), len(s.off)*4)
	w.raw(unsafe.Pointer(unsafe.SliceData(s.blob)), len(s.blob))
}

func (m *Map) WriteSnapshot(path string) {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	check(err)
	w := &snapWriter{w: bufio.NewWriterSize(f, 1<<20)}
	head := make([]byte, 16)
	w.w.Write(head)
	m.columns(w)
	check(w.w.Flush())
	copy(head, snapMagic)
	binary.LittleEndian.PutUint32(head[8:], snapVersion)
	binary.LittleEndian.PutUint32(head[12:], w.count)
	_, err = f.WriteAt(head, 0)
	check(err)
	check(f.Close())
	check(os.Rename(tmp, path))
}

type snapReader struct {
	buf   []byte
	pos   int
	count uint32
	err   error
}

func (r *snapReader) raw(elem int) (unsafe.Pointer, int) {
	if r.err != nil {
		return nil, 0
	}
	if r.pos+8 > len(r.buf) {
		r.err = fmt.Errorf("truncated at column %d", r.count)
		return nil, 0
	}
	size := int(binary.LittleEndian.Uint64(r.buf[r.pos:]))
	r.pos += 8
	if size%elem != 0 || size > len(r.buf)-r.pos {
		r.err = fmt.Errorf("bad column %d", r.count)
		return nil, 0
	}
	p := unsafe.Pointer(unsafe.SliceData(r.buf[r.pos:]))
	r.pos += (size + 7) &^ 7
	r.count++
	if size == 0 {
		return nil, 0
	}
	return p, size / elem
}

func (r *snapReader) i64(s *[]int64) {
	p, n := r.raw(8)
	*s = unsafe.Slice((*int64)(p), n)
}

func (r *snapReader) i32(s *[]int32) {
	p, n := r.raw(4)
	*s = unsafe.Slice((*int32)(p), n)
}

func (r *snapReader) strs(s *Strs) {
	p, n := r.raw(4)
	s.off = unsafe.Slice((*uint32)(p), n)
	p, n = r.raw(1)
	s.blob = unsafe.Slice((*byte)(p), n)
}

// LoadSnapshot maps the columns onto the file bytes (mmap and os.ReadFile memory are 8-aligned).
func LoadSnapshot(path string) (*Map, error) {
	buf, err := readMapped(path)
	if err != nil {
		return nil, err
	}
	if len(buf) < 16 || string(buf[:8]) != snapMagic {
		return nil, fmt.Errorf("%s: not a code map snapshot", path)
	}
	if v := binary.LittleEndian.Uint32(buf[8:]); v != snapVersion {
		return nil, fmt.Errorf("%s: snapshot format %d, expected %d: rebuild the map", path, v, snapVersion)
	}
	m := &Map{}
	r := &snapReader{buf: buf, pos: 16}
	m.columns(r)
	if r.err == nil && (r.count != binary.LittleEndian.Uint32(buf[12:]) || r.pos != len(buf)) {
		r.err = fmt.Errorf("column count or size mismatch")
	}
	if r.err != nil {
		return nil, fmt.Errorf("%s: %v", path, r.err)
	}
	return m, nil
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

// modulePathsJSON: the sources, resources and manifests of each module, for the stale check.
func modulePathsJSON(modules []Module) string {
	type paths struct {
		Sources   []string `json:"sources"`
		Res       []string `json:"res"`
		Manifests []string `json:"manifests"`
	}
	out := make([]paths, len(modules))
	for i, m := range modules {
		out[i] = paths{m.Sources, m.Res, m.Manifests}
	}
	data, _ := json.Marshal(out)
	return string(data)
}

func classDirsJSON(root string, modules []Module) string {
	set := map[string]bool{}
	for _, m := range modules {
		for _, d := range m.ClassDirs {
			if isDir(d) {
				r, _ := relPath(root, d)
				set[r] = true
			}
		}
	}
	out := sortedKeys(set)
	// json.dumps formatting: ", " between items
	parts := make([]string, len(out))
	for i, s := range out {
		bs, _ := json.Marshal(s)
		parts[i] = string(bs)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
