package main

import (
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
	m.Out, m.In = edgeCSR(edges, ids)
	m.Meta.Key.Add("nodes")
	m.Meta.Value.Add("3")

	if want := (CSR{Start: []int32{0, 3, 4, 5}, Node: []int32{1, 2, 2, 0, 0}, Kind: []int32{3, 0, 1, 1, 2}}); !reflect.DeepEqual(m.Out, want) {
		t.Fatalf("out %+v, want %+v", m.Out, want)
	}
	if want := (CSR{Start: []int32{0, 2, 3, 5}, Node: []int32{1, 2, 0, 0, 0}, Kind: []int32{1, 2, 3, 0, 1}}); !reflect.DeepEqual(m.In, want) {
		t.Fatalf("in %+v, want %+v", m.In, want)
	}

	path := filepath.Join(t.TempDir(), "map.snap")
	m.WriteSnapshot(path)
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
