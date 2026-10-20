// pkgload measures golang.org/x/tools/go/packages load cost in several modes.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"golang.org/x/tools/go/packages"
)

type edge struct {
	from, to uint32
	kind     uint8
}

func heapMB() float64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.HeapAlloc) / (1 << 20)
}

func main() {
	mode := flag.String("mode", "full", "full | export | names")
	dir := flag.String("dir", ".", "module dir")
	tests := flag.Bool("tests", false, "include tests")
	extract := flag.Bool("extract", false, "extract defs/uses into a compact table, drop packages, report retained heap")
	flag.Parse()
	pats := flag.Args()
	if len(pats) == 0 {
		pats = []string{"./..."}
	}

	var m packages.LoadMode
	switch *mode {
	case "full", "trim": // every package, deps included, type-checked from source
		m = packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedDeps |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule
	case "export": // workspace packages from source; deps from compiler export data (gopls-like)
		m = packages.NeedName | packages.NeedFiles | packages.NeedImports |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule
	case "names": // go list metadata only
		m = packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedDeps | packages.NeedModule
	}
	cfg := &packages.Config{Mode: m, Dir: *dir, Tests: *tests}
	if *mode == "trim" {
		cfg.ParseFile = trimParser(*dir)
	}
	t0 := time.Now()
	pkgs, err := packages.Load(cfg, pats...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	load := time.Since(t0)

	// count roots and everything reachable
	nRoots, nAll, nFiles, nErr := len(pkgs), 0, 0, 0
	seen := map[*packages.Package]bool{}
	var defs, uses int
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if seen[p] {
			return
		}
		seen[p] = true
		nAll++
		nFiles += len(p.GoFiles)
		nErr += len(p.Errors)
		if p.TypesInfo != nil {
			defs += len(p.TypesInfo.Defs)
			uses += len(p.TypesInfo.Uses)
		}
	})
	rootFiles := 0
	for _, p := range pkgs {
		rootFiles += len(p.GoFiles)
	}
	fmt.Printf("rootFiles=%d ", rootFiles)
	heapLoaded := heapMB()

	var heapAfter float64
	var nEdges, nSyms int
	var extractDur time.Duration
	if *extract {
		t1 := time.Now()
		ids := map[types.Object]uint32{}
		id := func(o types.Object) uint32 {
			if v, ok := ids[o]; ok {
				return v
			}
			v := uint32(len(ids))
			ids[o] = v
			return v
		}
		var edges []edge
		for _, p := range pkgs {
			if p.TypesInfo == nil {
				continue
			}
			for _, f := range p.Syntax {
				// enclosing func decl as "from"
				for _, d := range f.Decls {
					fd, ok := d.(*ast.FuncDecl)
					if !ok {
						continue
					}
					from := p.TypesInfo.Defs[fd.Name]
					if from == nil {
						continue
					}
					fid := id(from)
					ast.Inspect(fd, func(n ast.Node) bool {
						if ident, ok := n.(*ast.Ident); ok {
							if o := p.TypesInfo.Uses[ident]; o != nil {
								edges = append(edges, edge{fid, id(o), 0})
							}
						}
						return true
					})
				}
			}
		}
		nEdges, nSyms = len(edges), len(ids)
		extractDur = time.Since(t1)
		// keep only edges + a name table
		names := make([]string, len(ids))
		for o, v := range ids {
			if o.Pkg() != nil {
				names[v] = o.Pkg().Path() + "." + o.Name()
			} else {
				names[v] = o.Name()
			}
		}
		ids = nil
		pkgs = nil
		seen = nil
		debug.FreeOSMemory()
		heapAfter = heapMB()
		runtime.KeepAlive(edges)
		runtime.KeepAlive(names)
	}
	fmt.Printf("mode=%s dir=%s tests=%v load=%.2fs roots=%d pkgs=%d files=%d errs=%d defs=%d uses=%d heapLoaded=%.0fMB",
		*mode, *dir, *tests, load.Seconds(), nRoots, nAll, nFiles, nErr, defs, uses, heapLoaded)
	if *extract {
		fmt.Printf(" extract=%.0fms syms=%d edges=%d heapAfterDrop=%.1fMB", float64(extractDur.Microseconds())/1000, nSyms, nEdges, heapAfter)
	}
	fmt.Println()
}
