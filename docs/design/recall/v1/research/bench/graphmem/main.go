package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"sort"
)

type Edge struct {
	From, To   uint32 // symbol ids
	File, Line uint32 // evidence
	Kind       uint8
}

func heap() float64 { runtime.GC(); runtime.GC(); var m runtime.MemStats; runtime.ReadMemStats(&m); return float64(m.HeapAlloc) / (1 << 20) }

func main() {
	const E = 1_000_000
	const S = 200_000
	r := rand.New(rand.NewSource(1))
	h0 := heap()
	// layout A: flat sorted edge slice + reverse index of positions (CSR-like)
	edges := make([]Edge, E)
	for i := range edges {
		edges[i] = Edge{uint32(r.Intn(S)), uint32(r.Intn(S)), uint32(r.Intn(20000)), uint32(r.Intn(3000)), uint8(r.Intn(8))}
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].From < edges[j].From })
	fwd := make([]uint32, S+1)
	for _, e := range edges {
		fwd[e.From+1]++
	}
	for i := 1; i <= S; i++ {
		fwd[i] += fwd[i-1]
	}
	rev := make([]uint32, E)
	for i := range rev {
		rev[i] = uint32(i)
	}
	sort.Slice(rev, func(i, j int) bool { return edges[rev[i]].To < edges[rev[j]].To })
	names := make([]string, S)
	for i := range names {
		names[i] = fmt.Sprintf("github.com/example/service/internal/pkg%03d.Symbol%06d", i%300, i)
	}
	h1 := heap()
	fmt.Printf("flat CSR: 1M edges + 200k symbols (60-byte names): %.1f MB  (%.0f B/edge incl names)\n", h1-h0, (h1-h0)*(1<<20)/E)
	runtime.KeepAlive(edges); runtime.KeepAlive(fwd); runtime.KeepAlive(rev); runtime.KeepAlive(names)
	edges, fwd, rev = nil, nil, nil
	h2 := heap()
	// layout B: map-of-slices adjacency both directions (naive)
	out := map[uint32][]Edge{}
	in := map[uint32][]uint32{}
	for i := 0; i < E; i++ {
		e := Edge{uint32(r.Intn(S)), uint32(r.Intn(S)), uint32(r.Intn(20000)), uint32(r.Intn(3000)), uint8(r.Intn(8))}
		out[e.From] = append(out[e.From], e)
		in[e.To] = append(in[e.To], e.From)
	}
	h3 := heap()
	fmt.Printf("naive maps: 1M edges both directions (+names above kept): %.1f MB (%.0f B/edge)\n", h3-h2, (h3-h2)*(1<<20)/E)
	runtime.KeepAlive(out); runtime.KeepAlive(in); runtime.KeepAlive(names)
}
