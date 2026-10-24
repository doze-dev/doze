package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tsjs "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tsts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

func lang(name string) *sitter.Language {
	var p unsafe.Pointer
	switch name {
	case "go":
		p = tsgo.Language()
	case "ts":
		p = tsts.LanguageTypescript()
	case "tsx":
		p = tsts.LanguageTSX()
	case "js":
		p = tsjs.Language()
	}
	return sitter.NewLanguage(p)
}

func count(c *sitter.TreeCursor) int {
	n := 1
	if c.GotoFirstChild() {
		for {
			n += count(c)
			if !c.GotoNextSibling() {
				break
			}
		}
		c.GotoParent()
	}
	return n
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
	var bytes int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		b, err := os.ReadFile(sc.Text())
		if err == nil {
			srcs = append(srcs, b)
			paths = append(paths, sc.Text())
			bytes += len(b)
		}
	}
	lg := lang(*l)
	if *per {
		p := sitter.NewParser()
		p.SetLanguage(lg)
		for i, src := range srcs {
			ts0 := time.Now()
			tree := p.Parse(src, nil)
			d := time.Since(ts0)
			root := tree.RootNode()
			c := root.Walk()
			e := 0
			if root.HasError() {
				e = 1
			}
			fmt.Fprintf(perOut, "%s\t%d\t%d\t%d\t%d\n", paths[i], count(c), e, d.Microseconds(), len(src))
			c.Close()
			tree.Close()
		}
		return
	}
	var nodes, errs atomic.Int64
	var wg sync.WaitGroup
	ch := make(chan []byte)
	t1 := time.Now()
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := sitter.NewParser()
			defer p.Close()
			p.SetLanguage(lg)
			for src := range ch {
				tree := p.Parse(src, nil)
				root := tree.RootNode()
				c := root.Walk()
				nodes.Add(int64(count(c)))
				c.Close()
				if root.HasError() {
					errs.Add(1)
				}
				tree.Close()
			}
		}()
	}
	for _, s := range srcs {
		ch <- s
	}
	close(ch)
	wg.Wait()
	d := time.Since(t1)
	fmt.Printf("c-tree-sitter lang=%s j=%d files=%d MB=%.1f parse=%.2fs MB/s=%.1f nodes=%d errFiles=%d\n",
		*l, *workers, len(srcs), float64(bytes)/1e6, d.Seconds(), float64(bytes)/1e6/d.Seconds(), nodes.Load(), errs.Load())
}

var perOut = os.Stderr
