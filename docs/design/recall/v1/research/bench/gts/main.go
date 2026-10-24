package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func lang(name string) *ts.Language {
	switch name {
	case "go":
		return grammars.GoLanguage()
	case "ts":
		return grammars.TypescriptLanguage()
	case "tsx":
		return grammars.TsxLanguage()
	case "js":
		return grammars.JavascriptLanguage()
	}
	panic(name)
}

func count(n *ts.Node) int {
	c := 1
	for i := 0; i < n.ChildCount(); i++ {
		c += count(n.Child(i))
	}
	return c
}

func main() {
	l := flag.String("lang", "go", "")
	list := flag.String("list", "", "")
	workers := flag.Int("j", 1, "")
	per := flag.Bool("per", false, "")
	flag.Parse()
	f, _ := os.Open(*list)
	var srcs [][]byte
	var paths []string
	sc := bufio.NewScanner(f)
	var bytes int
	for sc.Scan() {
		b, err := os.ReadFile(sc.Text())
		if err == nil {
			srcs = append(srcs, b)
			paths = append(paths, sc.Text())
			bytes += len(b)
		}
	}
	t0 := time.Now()
	lg := lang(*l)
	p := ts.NewParser(lg)
	tr, _ := p.Parse([]byte("x"))
	if tr != nil {
		tr.Release()
	}
	load := time.Since(t0)
	if *per {
		for i, src := range srcs {
			if os.Getenv("FRESH") != "" && i%int(atoiDef(os.Getenv("FRESH"))) == 0 {
				p = ts.NewParser(lg)
			}
			ts0 := time.Now()
			tree, err := p.Parse(src)
			d := time.Since(ts0)
			if err != nil || tree == nil {
				fmt.Fprintf(perOut, "%s\t-1\t1\t%d\t%d\n", paths[i], d.Microseconds(), len(src))
				continue
			}
			e := 0
			if tree.RootNode().HasError() {
				e = 1
			}
			fmt.Fprintf(perOut, "%s\t%d\t%d\t%d\t%d\n", paths[i], count(tree.RootNode()), e, d.Microseconds(), len(src))
			tree.Release()
		}
		return
	}
	var nodes, errs, fails atomic.Int64
	var maxHeap atomic.Uint64
	var wg sync.WaitGroup
	ch := make(chan []byte)
	t1 := time.Now()
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := ts.NewParser(lg)
			k := 0
			for src := range ch {
				tree, err := p.Parse(src)
				if err != nil || tree == nil {
					fails.Add(1)
					continue
				}
				root := tree.RootNode()
				nodes.Add(int64(count(root)))
				if root.HasError() {
					errs.Add(1)
				}
				tree.Release()
				k++
				if k%50 == 0 {
					var m runtime.MemStats
					runtime.ReadMemStats(&m)
					if m.HeapInuse > maxHeap.Load() {
						maxHeap.Store(m.HeapInuse)
					}
				}
			}
		}()
	}
	for _, s := range srcs {
		ch <- s
	}
	close(ch)
	wg.Wait()
	d := time.Since(t1)
	fmt.Printf("gotreesitter lang=%s j=%d files=%d MB=%.1f grammarLoad=%v parse=%.2fs MB/s=%.1f nodes=%d errFiles=%d fails=%d maxHeapInuse=%dMB\n",
		*l, *workers, len(srcs), float64(bytes)/1e6, load.Round(time.Millisecond), d.Seconds(), float64(bytes)/1e6/d.Seconds(), nodes.Load(), errs.Load(), fails.Load(), maxHeap.Load()>>20)
}

var perOut = os.Stderr

func atoiDef(s string) int64 {
	var n int64
	fmt.Sscan(s, &n)
	if n <= 0 {
		n = 1
	}
	return n
}
