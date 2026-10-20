// stream type-checks workspace packages one at a time against export data,
// extracts edges, and drops syntax and TypesInfo before the next package.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"runtime"
	"sort"
	"time"

	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"
)

func heapMB() float64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.HeapAlloc) / (1 << 20)
}

type edge struct{ from, to uint32 }

func main() {
	dir := flag.String("dir", ".", "")
	tests := flag.Bool("tests", false, "")
	share := flag.Bool("share", true, "share imported types across packages")
	flag.Parse()
	t0 := time.Now()
	cfg := &packages.Config{Dir: *dir, Tests: *tests,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedDeps | packages.NeedExportFile | packages.NeedCompiledGoFiles}
	roots, err := packages.Load(cfg, "./...")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	tList := time.Since(t0)
	byPath := map[string]*packages.Package{}
	packages.Visit(roots, nil, func(p *packages.Package) { byPath[p.ID] = p })

	fset := token.NewFileSet()
	imported := map[string]*types.Package{}
	var maxHeap float64
	var edges []edge
	ids := map[string]uint32{}
	id := func(o types.Object) uint32 {
		k := o.Name()
		if o.Pkg() != nil {
			k = o.Pkg().Path() + "." + k
		}
		if v, ok := ids[k]; ok {
			return v
		}
		v := uint32(len(ids))
		ids[k] = v
		return v
	}
	type timing struct {
		id    string
		files int
		d     time.Duration
	}
	var times []timing
	t1 := time.Now()
	for _, r := range roots {
		ts := time.Now()
		if !*share {
			imported = map[string]*types.Package{}
			fset = token.NewFileSet()
		}
		imp := gcexportdata.NewImporter(fset, imported)
		lookup := func(path string) (*packages.Package, bool) {
			p, ok := r.Imports[path]
			return p, ok
		}
		var files []*ast.File
		for _, fn := range r.CompiledGoFiles {
			f, err := parser.ParseFile(fset, fn, nil, parser.SkipObjectResolution)
			if err == nil {
				files = append(files, f)
			}
		}
		info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
		tc := &types.Config{Error: func(error) {}, Importer: importerFunc(func(path string) (*types.Package, error) {
			p, ok := lookup(path)
			if !ok || p.ExportFile == "" {
				return nil, fmt.Errorf("no export for %s", path)
			}
			if tp, ok := imported[p.PkgPath]; ok && tp.Complete() {
				return tp, nil
			}
			f, err := os.Open(p.ExportFile)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			rd, err := gcexportdata.NewReader(f)
			if err != nil {
				return nil, err
			}
			return gcexportdata.Read(rd, fset, imported, p.PkgPath)
		})}
		_ = imp
		tc.Check(r.PkgPath, fset, files, info)
		for _, f := range files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || info.Defs[fd.Name] == nil {
					continue
				}
				fid := id(info.Defs[fd.Name])
				ast.Inspect(fd, func(n ast.Node) bool {
					if ident, ok := n.(*ast.Ident); ok {
						if o := info.Uses[ident]; o != nil {
							edges = append(edges, edge{fid, id(o)})
						}
					}
					return true
				})
			}
		}
		times = append(times, timing{r.ID, len(files), time.Since(ts)})
		if h := heapMB(); h > maxHeap {
			maxHeap = h
		}
	}
	tCheck := time.Since(t1)
	sort.Slice(times, func(i, j int) bool { return times[i].d > times[j].d })
	var tot time.Duration
	for _, t := range times {
		tot += t.d
	}
	med := times[len(times)/2]
	fmt.Printf("roots=%d list=%.2fs checkSerial=%.2fs edges=%d syms=%d maxHeapBetweenPkgs=%.0fMB endHeap=%.0fMB imported=%d\n",
		len(roots), tList.Seconds(), tCheck.Seconds(), len(edges), len(ids), maxHeap, heapMB(), len(imported))
	fmt.Printf("per-package check: median=%v (%d files) max=%v %s (%d files)\n", med.d.Round(time.Millisecond), med.files, times[0].d.Round(time.Millisecond), times[0].id, times[0].files)
	for _, t := range times[:5] {
		fmt.Printf("  %v %s files=%d\n", t.d.Round(time.Millisecond), t.id, t.files)
	}
}

type importerFunc func(string) (*types.Package, error)

func (f importerFunc) Import(p string) (*types.Package, error) { return f(p) }
