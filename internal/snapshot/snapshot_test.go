package snapshot

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"unsafe"
)

func TestSnapshotRoundTrip(t *testing.T) {
	ids := []int64{10, 20, 30}
	edges := map[Edge]struct{}{{30, 10, 2}: {}, {10, 30, 1}: {}, {10, 20, 3}: {}, {10, 30, 0}: {}, {20, 10, 1}: {}}
	m := &Map{}
	m.Node.ID = ids
	m.Node.Owner = []int64{0, 10, 10}
	for _, s := range []string{"c", "m", "f"} {
		m.Node.Kind.Add(s)
		m.Node.Name.Add("n" + s)
		m.Node.Desc.Add("")
	}
	m.Out, m.In, _ = EdgeCSR(edges, ids, nil)
	m.Meta.Key.Add("nodes")
	m.Meta.Value.Add("3")

	if want := (CSR{Start: []int32{0, 3, 4, 5}, Node: []int32{1, 2, 2, 0, 0}, Kind: []int32{3, 0, 1, 1, 2}}); !reflect.DeepEqual(m.Out, want) {
		t.Fatalf("out %+v, want %+v", m.Out, want)
	}
	if want := (CSR{Start: []int32{0, 2, 3, 5}, Node: []int32{1, 2, 0, 0, 0}, Kind: []int32{1, 2, 3, 0, 1}}); !reflect.DeepEqual(m.In, want) {
		t.Fatalf("in %+v, want %+v", m.In, want)
	}

	path := filepath.Join(t.TempDir(), "map.snap")
	if err := m.WriteSnapshot(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, m) {
		t.Fatalf("loaded %+v\nwant %+v", got, m)
	}
	if got.Node.Name.At(2) != "nf" || got.Node.Desc.At(1) != "" || got.MetaValue("nodes") != "3" {
		t.Fatal("strings do not round-trip")
	}
}

// A corrupt or truncated file is an error, never a panic: the column sizes reach unsafe.Slice.
func TestSnapshotCorrupt(t *testing.T) {
	m := &Map{}
	m.Node.ID = []int64{1, 2}
	m.Meta.Key.Add("nodes")
	m.Meta.Value.Add("2")
	path := filepath.Join(t.TempDir(), "map.snap")
	if err := m.WriteSnapshot(path); err != nil {
		t.Fatal(err)
	}
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, corrupt := range map[string]func([]byte) []byte{
		"negative size": func(b []byte) []byte { binary.LittleEndian.PutUint64(b[16:], 1<<63|8); return b },
		"huge size":     func(b []byte) []byte { binary.LittleEndian.PutUint64(b[16:], 1<<40); return b },
		"odd size":      func(b []byte) []byte { binary.LittleEndian.PutUint64(b[16:], 3); return b },
		"truncated":     func(b []byte) []byte { return b[:len(b)-5] },
		"no columns":    func(b []byte) []byte { return b[:16] },
	} {
		bad := corrupt(append([]byte(nil), good...))
		p := filepath.Join(t.TempDir(), "bad.snap")
		if err := os.WriteFile(p, bad, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSnapshot(p); err == nil {
			t.Errorf("%s: loaded without an error", name)
		}
	}
}

// FuzzLoad: any bytes load or fail with an error, never a panic. Run it with a memory limit:
// `(ulimit -v 4000000; go test ./internal/snapshot -fuzz FuzzLoad -fuzztime 60s)`.
func FuzzLoad(f *testing.F) {
	m := &Map{}
	m.Node.ID = []int64{10, 20}
	m.Node.Owner = []int64{0, 10}
	m.Node.Kind.Add("c")
	m.Node.Kind.Add("m")
	m.Out, m.In, m.OutLine = EdgeCSR(map[Edge]struct{}{{10, 20, 1}: {}}, m.Node.ID, map[Edge]int32{{10, 20, 1}: 7})
	m.Meta.Key.Add("nodes")
	m.Meta.Value.Add("2")
	path := filepath.Join(f.TempDir(), "map.snap")
	if err := m.WriteSnapshot(path); err != nil {
		f.Fatal(err)
	}
	good, err := os.ReadFile(path)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(good)
	f.Fuzz(func(t *testing.T, data []byte) {
		// the columns are views of the buffer: copied into int64s for the alignment a mapped file has
		buf := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(make([]int64, len(data)/8+1)))), len(data))
		copy(buf, data)
		loadBytes(buf)
	})
}

// A map of an older build lacks a column, one of a newer build has a column this binary does not
// know: both load, the lacking column empty and named in Missing.
func TestColumnsByName(t *testing.T) {
	m := &Map{}
	m.Node.ID = []int64{10, 20}
	m.Out, m.In, m.OutLine = EdgeCSR(map[Edge]struct{}{{10, 20, 1}: {}}, m.Node.ID, map[Edge]int32{{10, 20, 1}: 7})
	path := filepath.Join(t.TempDir(), "map.snap")
	if err := m.WriteSnapshot(path); err != nil {
		t.Fatal(err)
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cols, err := index(buf, binary.LittleEndian.Uint32(buf[12:]))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w := &snapWriter{w: bufio.NewWriter(&out)}
	w.w.Write(buf[:16])
	for _, name := range slices.Sorted(maps.Keys(cols)) {
		if name != "out.line" {
			w.raw(name, cols[name].p, cols[name].size)
		}
	}
	future := []int64{1, 2, 3}
	w.i64("future.column", &future)
	w.w.Flush()
	b := out.Bytes()
	binary.LittleEndian.PutUint32(b[12:], w.count)
	aligned := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(make([]int64, len(b)/8+1)))), len(b))
	copy(aligned, b)
	got, err := loadBytes(aligned)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Missing, []string{"out.line"}) || got.OutLine != nil || !slices.Equal(got.Node.ID, m.Node.ID) ||
		!reflect.DeepEqual(got.Out, m.Out) {
		t.Fatalf("missing %v, out.line %v, node.id %v", got.Missing, got.OutLine, got.Node.ID)
	}
}
