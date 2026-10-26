package snapshot

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
	m.Out, m.In = EdgeCSR(edges, ids)
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
