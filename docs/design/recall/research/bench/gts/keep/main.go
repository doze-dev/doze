package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func heap() uint64 { runtime.GC(); runtime.GC(); var m runtime.MemStats; runtime.ReadMemStats(&m); return m.HeapAlloc }

func main() {
	f, _ := os.Open(os.Args[1])
	sc := bufio.NewScanner(f)
	var srcs [][]byte
	n := 0
	for sc.Scan() {
		b, _ := os.ReadFile(sc.Text())
		srcs = append(srcs, b)
		n += len(b)
	}
	p := ts.NewParser(grammars.GoLanguage())
	t, _ := p.Parse([]byte("package x"))
	t.Release()
	h0 := heap()
	var trees []*ts.Tree
	nodes := 0
	for _, s := range srcs {
		t, _ := p.Parse(s)
		trees = append(trees, t)
	}
	h1 := heap()
	_ = nodes
	fmt.Printf("retained trees: %d files, %.1f MB source, heap +%.0f MB, %.1f bytes heap per source byte\n", len(trees), float64(n)/1e6, float64(h1-h0)/(1<<20), float64(h1-h0)/float64(n))
	runtime.KeepAlive(trees)
}
