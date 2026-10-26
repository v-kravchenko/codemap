package main

import (
	"runtime"
	"slices"
	"sort"
	"sync"
)

// derive: what queries need without lookups: node indexes of owners and tops, the members of each
// class, repo supertypes by index, the overriders of each method.
func (m *Map) derive() {
	n := m.Nodes()
	nd := &m.Node
	nd.OwnerIx, nd.TopIx = make([]int32, n), make([]int32, n)
	chunks(n, func(lo, hi int) {
		for i := lo; i < hi; i++ {
			nd.OwnerIx[i], nd.TopIx[i] = m.Index(nd.Owner[i]), m.Index(nd.Top[i])
		}
	})
	m.Members = groupCSR(n, nd.OwnerIx)
	classes := m.ClassIndex()
	h := &m.Hierarchy
	h.ClsIx, h.SupIx = make([]int32, len(h.Cls)), make([]int32, len(h.Cls))
	for r := range h.Cls {
		h.ClsIx[r] = m.Index(h.Cls[r])
		if s, ok := classes[h.Sup.At(r)]; ok {
			h.SupIx[r] = s
		} else {
			h.SupIx[r] = -1
		}
	}
	m.Overriders = m.overriders()
}

// Index: the node index of an id, -1 for none.
func (m *Map) Index(id int64) int32 {
	if id == 0 {
		return -1
	}
	i, ok := slices.BinarySearch(m.Node.ID, id)
	if !ok {
		return -1
	}
	return int32(i)
}

func (m *Map) ClassIndex() map[string]int32 {
	out := make(map[string]int32, 40000)
	for i := range m.Nodes() {
		if m.Node.Kind.At(i) == "C" {
			out[m.Node.Name.At(i)] = int32(i)
		}
	}
	return out
}

// SuperRows: the hierarchy rows of each class, in row order.
func (m *Map) SuperRows() CSR { return groupCSR(m.Nodes(), m.Hierarchy.ClsIx) }

// Supertypes: repo supertypes of class c, transitively, in the order of graph.py supertypes().
func (m *Map) Supertypes(rows CSR, c int32) []int32 {
	var out []int32
	var stack []int32
	push := func(c int32) {
		for _, r := range rows.Node[rows.Start[c]:rows.Start[c+1]] {
			stack = append(stack, m.Hierarchy.SupIx[r])
		}
	}
	push(c)
	seen := map[int32]bool{}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if s < 0 || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
		push(s)
	}
	return out
}

// overriders: for a method m, the classes o with a method of the same name and descriptor, o != the
// class of m, the class of m among the supertypes of o (graph.py _overriders).
func (m *Map) overriders() CSR {
	type sig struct{ name, desc string }
	groups := map[sig][]int32{}
	for i := range m.Nodes() {
		if m.Node.Kind.At(i) == "M" {
			k := sig{m.Node.Name.At(i), m.Node.Desc.At(i)}
			groups[k] = append(groups[k], int32(i))
		}
	}
	rows := m.SuperRows()
	sups := map[int32][]int32{}
	var meth, cls []int32
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		owners := make(map[int32]int32, len(g))
		for _, o := range g {
			owners[m.Node.OwnerIx[o]] = o
		}
		for _, o := range g {
			c := m.Node.OwnerIx[o]
			if c < 0 {
				continue
			}
			st, ok := sups[c]
			if !ok {
				st = m.Supertypes(rows, c)
				sups[c] = st
			}
			for _, s := range st {
				if mm, ok := owners[s]; ok && s != c {
					meth, cls = append(meth, mm), append(cls, c)
				}
			}
		}
	}
	order := make([]int32, len(meth))
	for i := range order {
		order[i] = int32(i)
	}
	order, start := countingSort(order, meth, m.Nodes())
	out := CSR{Start: start, Node: make([]int32, len(order))}
	for i, e := range order {
		out.Node[i] = cls[e]
	}
	for i := range m.Nodes() {
		seg := out.Node[start[i]:start[i+1]]
		sort.Slice(seg, func(a, b int) bool { return seg[a] < seg[b] })
	}
	return out
}

// groupCSR: the indexes i in [0, len(key)) grouped by key[i] in [0, n) (-1: left out), in index order.
func groupCSR(n int, key []int32) CSR {
	start := make([]int32, n+1)
	for _, k := range key {
		if k >= 0 {
			start[k+1]++
		}
	}
	for k := 1; k <= n; k++ {
		start[k] += start[k-1]
	}
	pos := slices.Clone(start)
	out := make([]int32, start[n])
	for i, k := range key {
		if k >= 0 {
			out[pos[k]] = int32(i)
			pos[k]++
		}
	}
	return CSR{Start: start, Node: out}
}

func chunks(n int, f func(lo, hi int)) {
	parts := runtime.NumCPU()
	var wg sync.WaitGroup
	for p := range parts {
		lo, hi := n*p/parts, n*(p+1)/parts
		wg.Go(func() { f(lo, hi) })
	}
	wg.Wait()
}
