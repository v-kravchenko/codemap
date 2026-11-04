package snapshot

// The map as flat sorted columns, and their binary snapshot: what the CLI loads.
//
// Snapshot: magic, format version, column count (u32 each), then each column as a u64 byte length
// and its bytes padded to 8. Numbers are little-endian (every target: amd64, arm64). Loading maps
// the columns onto the file bytes without copies.

import (
	"bufio"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"unsafe"

	"github.com/v-kravchenko/codemap/internal/util"
)

const (
	snapMagic   = "CODEMAP\x00"
	snapVersion = 4 // the file's layout: named columns since 4; a new column needs no new version
)

// Strs: strings of a column, s[i] = blob[off[i]:off[i+1]].
// No dedup: ~41 of 75 MB are repeated descs/paths; a shared string pool if load time matters.
type Strs struct {
	Off  []uint32
	Blob []byte
}

func (s *Strs) Add(v string) {
	if s.Off == nil {
		s.Off = []uint32{0}
	}
	s.Blob = append(s.Blob, v...)
	s.Off = append(s.Off, uint32(len(s.Blob)))
}

func (s *Strs) At(i int) string {
	b := s.Blob[s.Off[i]:s.Off[i+1]]
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func (s *Strs) Len() int { return max(len(s.Off)-1, 0) }

// CSR: edges of node i are Node[Start[i]:Start[i+1]] (node indexes), with their Kind.
type CSR struct{ Start, Node, Kind []int32 }

// Map: the tables of the map; ids are node ids, node rows are sorted by id. Edges are kept
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
	OutLine    []int32    // the first line of the code of each Out edge, 0: none
	Missing    []string   // the columns the file lacks (built by an older binary), read empty; not written
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
	ScreenContent struct{ Screen, Node []int64 } // screen -> the composables its own source code draws
	Entry         struct {
		Node         []int64
		Kind, Detail Strs
		File, Line   []int32
	}
	Enclosing struct { // a lambda or anonymous class -> its EnclosingMethod attribute (Method "": none)
		Cls           []int64
		Outer, Method Strs
	}
	Component struct { // application, service, receiver, provider (the manifest), worker (worker_bases)
		Node          []int64
		Kind, Actions Strs // Actions: of its intent filters, sorted, comma-separated
	}
}

// OpenedRows: the ways each screen is opened (precomputed by build); Hub, HostNode -1: none.
type OpenedRows struct {
	Screen, Src, Hub, HostNode []int32
	HostModule                 Strs
	Via                        CSR
	// Shared, per row: the nodes every shortest way from Src passes, nearest Src first, each where
	// more sources share it; Sources: how many
	Shared struct{ Start, Node, Sources []int32 }
}

// BaseRows: base classes that open a screen, with the screens extending them (opened_base).
type BaseRows struct {
	Screen, Base []int32
	Below        CSR
}

type codec interface {
	i64(string, *[]int64)
	i32(string, *[]int32)
	strs(string, *Strs)
}

// columns lists every column by name; writing and reading both walk it. A column is only added under
// a new name (an old map lacks it, an old binary skips it); its meaning never changes under the same
// name (docs/decisions/0036).
func (m *Map) columns(c codec) {
	c.strs("meta.key", &m.Meta.Key)
	c.strs("meta.value", &m.Meta.Value)
	c.strs("module.name", &m.Module.Name)
	c.strs("module.dir", &m.Module.Dir)
	f := &m.File
	c.i32("file.id", &f.ID)
	c.i32("file.module", &f.Module)
	c.i32("file.lines", &f.Lines)
	c.strs("file.path", &f.Path)
	n := &m.Node
	c.i64("node.id", &n.ID)
	c.i64("node.owner", &n.Owner)
	c.i64("node.top", &n.Top)
	c.strs("node.kind", &n.Kind)
	c.strs("node.name", &n.Name)
	c.strs("node.desc", &n.Desc)
	c.strs("node.sname", &n.Sname)
	c.i32("node.flags", &n.Flags)
	c.i32("node.file", &n.File)
	c.i32("node.module", &n.Module)
	c.i32("node.lmin", &n.LMin)
	c.i32("node.lmax", &n.LMax)
	c.i32("node.ownerix", &n.OwnerIx)
	c.i32("node.topix", &n.TopIx)
	c.i32("out.start", &m.Out.Start)
	c.i32("out.node", &m.Out.Node)
	c.i32("out.kind", &m.Out.Kind)
	c.i32("in.start", &m.In.Start)
	c.i32("in.node", &m.In.Node)
	c.i32("in.kind", &m.In.Kind)
	c.i32("members.start", &m.Members.Start)
	c.i32("members.node", &m.Members.Node)
	c.i32("members.kind", &m.Members.Kind)
	c.i32("overriders.start", &m.Overriders.Start)
	c.i32("overriders.node", &m.Overriders.Node)
	c.i32("overriders.kind", &m.Overriders.Kind)
	c.i32("out.line", &m.OutLine)
	c.i64("callvia.src", &m.CallVia.Src)
	c.i64("callvia.dst", &m.CallVia.Dst)
	c.strs("callvia.via", &m.CallVia.Via)
	c.i64("hierarchy.cls", &m.Hierarchy.Cls)
	c.strs("hierarchy.sup", &m.Hierarchy.Sup)
	c.i32("hierarchy.kind", &m.Hierarchy.Kind)
	c.i32("hierarchy.clsix", &m.Hierarchy.ClsIx)
	c.i32("hierarchy.supix", &m.Hierarchy.SupIx)
	ob := &m.OpenedBy
	c.i32("openedby.screen", &ob.Screen)
	c.i32("openedby.src", &ob.Src)
	c.i32("openedby.hub", &ob.Hub)
	c.i32("openedby.hostnode", &ob.HostNode)
	c.i32("openedby.via.start", &ob.Via.Start)
	c.i32("openedby.via.node", &ob.Via.Node)
	c.i32("openedby.shared.start", &ob.Shared.Start)
	c.i32("openedby.shared.node", &ob.Shared.Node)
	c.i32("openedby.shared.sources", &ob.Shared.Sources)
	c.strs("openedby.hostmodule", &ob.HostModule)
	b := &m.OpenedBase
	c.i32("openedbase.screen", &b.Screen)
	c.i32("openedbase.base", &b.Base)
	c.i32("openedbase.below.start", &b.Below.Start)
	c.i32("openedbase.below.node", &b.Below.Node)
	c.i64("annotation.node", &m.Annotation.Node)
	c.strs("annotation.name", &m.Annotation.Name)
	c.i64("resdef.node", &m.ResDef.Node)
	c.i32("resdef.file", &m.ResDef.File)
	c.i32("resdef.line", &m.ResDef.Line)
	cs := &m.CallSite
	c.i64("callsite.src", &cs.Src)
	c.i64("callsite.dst", &cs.Dst)
	c.i64("callsite.arg", &cs.Arg)
	c.i32("callsite.site", &cs.Site)
	c.i32("callsite.pos", &cs.Pos)
	c.strs("callsite.kind", &cs.Kind)
	c.i64("paramswitch.method", &m.ParamSwitch.Method)
	c.i64("paramswitch.entry", &m.ParamSwitch.Entry)
	c.i64("paramswitch.pos", &m.ParamSwitch.Pos)
	c.i64("directread.method", &m.DirectRead.Method)
	c.i64("directread.entry", &m.DirectRead.Entry)
	g := &m.DefaultGuard
	c.i64("defaultguard.method", &g.Method)
	c.i64("defaultguard.dst", &g.Dst)
	c.i32("defaultguard.kind", &g.Kind)
	c.i32("defaultguard.pos", &g.Pos)
	c.i32("defaultguard.bit", &g.Bit)
	c.strs("input.path", &m.Input.Path)
	c.strs("input.sha1", &m.Input.Sha1)
	c.i64("input.size", &m.Input.Size)
	c.i64("input.mtime", &m.Input.Mtime)
	s := &m.Screen
	c.i64("screen.node", &s.Node)
	c.i64("screen.host", &s.Host)
	c.strs("screen.kind", &s.Kind)
	c.strs("screen.label", &s.Label)
	c.strs("screen.target", &s.Target)
	c.i64("screenrelated.screen", &m.ScreenRelated.Screen)
	c.i64("screenrelated.related", &m.ScreenRelated.Related)
	c.i64("screencontent.screen", &m.ScreenContent.Screen)
	c.i64("screencontent.node", &m.ScreenContent.Node)
	e := &m.Entry
	c.i64("entry.node", &e.Node)
	c.strs("entry.kind", &e.Kind)
	c.strs("entry.detail", &e.Detail)
	c.i32("entry.file", &e.File)
	c.i32("entry.line", &e.Line)
	c.i64("component.node", &m.Component.Node)
	c.strs("component.kind", &m.Component.Kind)
	c.strs("component.actions", &m.Component.Actions)
	c.i64("enclosing.cls", &m.Enclosing.Cls)
	c.strs("enclosing.outer", &m.Enclosing.Outer)
	c.strs("enclosing.method", &m.Enclosing.Method)
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

func SortedPairs(set map[[2]int64]struct{}) [][2]int64 {
	out := make([][2]int64, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i][0] < out[j][0] || out[i][0] == out[j][0] && out[i][1] < out[j][1]
	})
	return out
}

// edgeCSR sorts the edges by radix passes over node indexes (kind, dst, src: stable), no comparisons;
// outLine: the line of each Out edge from lines.
func EdgeCSR(edges map[Edge]struct{}, ids []int64, lines map[Edge]int32) (out, in CSR, outLine []int32) {
	idx := make(map[int64]int32, len(ids))
	for i, id := range ids {
		idx[id] = int32(i)
	}
	m := len(edges)
	src, dst, kind, line := make([]int32, m), make([]int32, m), make([]int32, m), make([]int32, m)
	maxKind := 0
	i := 0
	for e := range edges {
		s, ok1 := idx[e.Src]
		d, ok2 := idx[e.Dst]
		if !ok1 || !ok2 || e.Kind < 0 {
			panic(fmt.Sprintf("edge %d -> %d (kind %d): no such node", e.Src, e.Dst, e.Kind))
		}
		src[i], dst[i], kind[i], line[i] = s, d, int32(e.Kind), lines[e]
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
	outLine = make([]int32, m)
	for i, e := range order {
		out.Node[i], out.Kind[i], outLine[i] = dst[e], kind[e], line[e]
	}
	order, inStart := countingSort(order, dst, n)
	in = CSR{Start: inStart, Node: make([]int32, m), Kind: make([]int32, m)}
	for i, e := range order {
		in.Node[i], in.Kind[i] = src[e], kind[e]
	}
	return out, in, outLine
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

// raw: a column: its name's length, the name, its size, its bytes, each padded to 8.
func (w *snapWriter) raw(name string, p unsafe.Pointer, size int) {
	w.w.Write(binary.LittleEndian.AppendUint64(nil, uint64(len(name))))
	w.w.WriteString(name)
	w.w.Write(zeros[:(8-len(name)%8)%8])
	w.w.Write(binary.LittleEndian.AppendUint64(nil, uint64(size)))
	if size > 0 {
		w.w.Write(unsafe.Slice((*byte)(p), size))
	}
	pad := (8 - size%8) % 8
	w.w.Write(zeros[:pad])
	w.count++
}

func (w *snapWriter) i64(name string, s *[]int64) {
	w.raw(name, unsafe.Pointer(unsafe.SliceData(*s)), len(*s)*8)
}
func (w *snapWriter) i32(name string, s *[]int32) {
	w.raw(name, unsafe.Pointer(unsafe.SliceData(*s)), len(*s)*4)
}
func (w *snapWriter) strs(name string, s *Strs) {
	if s.Off == nil {
		s.Off = []uint32{0}
	}
	w.raw(name+".off", unsafe.Pointer(unsafe.SliceData(s.Off)), len(s.Off)*4)
	w.raw(name+".blob", unsafe.Pointer(unsafe.SliceData(s.Blob)), len(s.Blob))
}

// WriteSnapshot writes to a temp file renamed over path, so a failed write (disk full) leaves the
// old map in place and no temp file behind; two builds at once each write a temp file of their own.
func (m *Map) WriteSnapshot(path string) (err error) {
	f, err := util.CreateTemp(path)
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	w := &snapWriter{w: bufio.NewWriterSize(f, 1<<20)}
	head := make([]byte, 16)
	w.w.Write(head)
	m.columns(w)
	if err = w.w.Flush(); err != nil {
		return err
	}
	copy(head, snapMagic)
	binary.LittleEndian.PutUint32(head[8:], snapVersion)
	binary.LittleEndian.PutUint32(head[12:], w.count)
	if _, err = f.WriteAt(head, 0); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp, 0o644); err != nil { // CreateTemp makes it 0600
		return err
	}
	return os.Rename(tmp, path)
}

type column struct {
	p    unsafe.Pointer
	size int
}

// snapReader: the columns of a map by name; missing: the names a map lacks (built by an older
// binary), its columns left empty.
type snapReader struct {
	cols    map[string]column
	missing []string
}

// index: every column of buf after the header, by name; a name this binary does not know is kept
// and never read.
func index(buf []byte, count uint32) (map[string]column, error) {
	cols := map[string]column{}
	pos := 16
	// compared unsigned: a corrupt size past int's range would turn negative and reach unsafe.Slice
	next := func(i int) (int, error) {
		if pos+8 > len(buf) {
			return 0, fmt.Errorf("truncated at column %d", i)
		}
		u := binary.LittleEndian.Uint64(buf[pos:])
		pos += 8
		if u > uint64(len(buf)-pos) {
			return 0, fmt.Errorf("bad column %d", i)
		}
		return int(u), nil
	}
	for i := 0; pos < len(buf); i++ {
		n, err := next(i)
		if err != nil {
			return nil, err
		}
		name := string(buf[pos : pos+n])
		pos += (n + 7) &^ 7
		size, err := next(i)
		if err != nil {
			return nil, err
		}
		if pos+size > len(buf) {
			return nil, fmt.Errorf("bad column %d", i)
		}
		cols[name] = column{unsafe.Pointer(unsafe.SliceData(buf[pos:])), size}
		pos += (size + 7) &^ 7
	}
	if pos != len(buf) || uint32(len(cols)) != count {
		return nil, fmt.Errorf("column count or size mismatch")
	}
	return cols, nil
}

func (r *snapReader) raw(name string, elem int) (unsafe.Pointer, int) {
	c, ok := r.cols[name]
	if !ok {
		r.missing = append(r.missing, name)
		return nil, 0
	}
	if c.size%elem != 0 || c.size == 0 {
		return nil, 0
	}
	return c.p, c.size / elem
}

func (r *snapReader) i64(name string, s *[]int64) {
	p, n := r.raw(name, 8)
	*s = unsafe.Slice((*int64)(p), n)
}

func (r *snapReader) i32(name string, s *[]int32) {
	p, n := r.raw(name, 4)
	*s = unsafe.Slice((*int32)(p), n)
}

func (r *snapReader) strs(name string, s *Strs) {
	p, n := r.raw(name+".off", 4)
	s.Off = unsafe.Slice((*uint32)(p), n)
	p, n = r.raw(name+".blob", 1)
	s.Blob = unsafe.Slice((*byte)(p), n)
}

// LoadSnapshot maps the columns onto the file bytes (mmap and os.ReadFile memory are 8-aligned).
func LoadSnapshot(path string) (*Map, error) {
	buf, err := readMapped(path)
	if err != nil {
		return nil, err
	}
	m, err := loadBytes(buf)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return m, nil
}

// loadBytes: the map over buf, which must be 8-aligned.
func loadBytes(buf []byte) (*Map, error) {
	if len(buf) < 16 || string(buf[:8]) != snapMagic {
		return nil, fmt.Errorf("not a code map snapshot")
	}
	if v := binary.LittleEndian.Uint32(buf[8:]); v != snapVersion {
		return nil, fmt.Errorf("snapshot format %d, expected %d: rebuild the map", v, snapVersion)
	}
	cols, err := index(buf, binary.LittleEndian.Uint32(buf[12:]))
	if err != nil {
		return nil, err
	}
	m := &Map{}
	r := &snapReader{cols: cols}
	m.columns(r)
	m.Missing = r.missing
	return m, nil
}

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
	AccEnum      = 0x4000
	AccStatic    = 0x0008
	AccAbstract  = 0x0400
	AccInterface = 0x0200
	ConstantFlag = 0x10000
)

// IndexVersion: MAJOR.MINOR of what the map holds. A query refuses a map of another major (a column
// changed its meaning) and reads one of another minor: a column a newer build added is skipped, one
// an older build lacks reads empty (Missing), and the answer says so (docs/decisions/0036).
const IndexVersion = "14.0"

// IndexCompatible: whether a map of index version v can be read by this binary.
func IndexCompatible(v string) bool {
	major, _, _ := strings.Cut(v, ".")
	mine, _, _ := strings.Cut(IndexVersion, ".")
	return major == mine
}

func ToolSha1() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return ""
	}
	s := sha1.Sum(data)
	return hex.EncodeToString(s[:])
}

type Edge struct {
	Src, Dst int64
	Kind     int
}
