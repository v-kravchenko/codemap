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
	"unsafe"

	"github.com/v-kravchenko/codemap/internal/util"
)

const (
	snapMagic   = "CODEMAP\x00"
	snapVersion = 3
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
	ScreenContent struct{ Screen, Node []int64 } // route -> the composables its own source code draws
	Entry         struct {
		Node         []int64
		Kind, Detail Strs
		File, Line   []int32
	}
	Component struct { // application, service, receiver, provider (the manifest), worker (worker_bases)
		Node []int64
		Kind Strs
	}
}

// OpenedRows: the ways each screen is opened (precomputed by build); Hub, HostNode -1: none.
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
	c.i64(&m.ScreenContent.Screen)
	c.i64(&m.ScreenContent.Node)
	e := &m.Entry
	c.i64(&e.Node)
	c.strs(&e.Kind)
	c.strs(&e.Detail)
	c.i32(&e.File)
	c.i32(&e.Line)
	c.i64(&m.Component.Node)
	c.strs(&m.Component.Kind)
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

// edgeCSR sorts the edges by radix passes over node indexes (kind, dst, src: stable), no comparisons.
func EdgeCSR(edges map[Edge]struct{}, ids []int64) (out, in CSR) {
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
	if s.Off == nil {
		s.Off = []uint32{0}
	}
	w.raw(unsafe.Pointer(unsafe.SliceData(s.Off)), len(s.Off)*4)
	w.raw(unsafe.Pointer(unsafe.SliceData(s.Blob)), len(s.Blob))
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
	// compared unsigned: a corrupt size past int's range would turn negative and reach unsafe.Slice
	usize := binary.LittleEndian.Uint64(r.buf[r.pos:])
	r.pos += 8
	if usize%uint64(elem) != 0 || usize > uint64(len(r.buf)-r.pos) {
		r.err = fmt.Errorf("bad column %d", r.count)
		return nil, 0
	}
	size := int(usize)
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
	s.Off = unsafe.Slice((*uint32)(p), n)
	p, n = r.raw(1)
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
	m := &Map{}
	r := &snapReader{buf: buf, pos: 16}
	m.columns(r)
	if r.err == nil && (r.count != binary.LittleEndian.Uint32(buf[12:]) || r.pos != len(buf)) {
		r.err = fmt.Errorf("column count or size mismatch")
	}
	if r.err != nil {
		return nil, r.err
	}
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

const IndexVersion = "12"

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
