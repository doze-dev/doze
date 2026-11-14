// oracle-dump (rust-oracle branch only, never shipped): writes what Go's HCL,
// go-cty and internal/config make of a corpus as JSON, so doze-rs's doze-hcl
// and doze-config can be diffed against it line by line.
//
//	oracle-dump parse  <dir>            syntax accept/reject and diagnostics per *.hcl
//	oracle-dump syntax <dir>            blocks and attributes per file, with lines
//	oracle-dump refs   <dir>            variable traversals per attribute
//	oracle-dump fmt    <dir> <outdir>   hclwrite.Format of each accepted file
//	oracle-dump edit   <dir> <outdir>   internal/config's AppendBlock path per file
//	oracle-dump eval   <exprs.json>     value, exact type and diagnostics per expression,
//	                                    with doze.hcl's function table
//	oracle-dump convert <cases.json> each value converted to a cty type (JSON), or types unified
//	oracle-dump config [-shape] <path>… each path loaded as doze loads it (a file, or a
//	                                    directory's *.hcl): the Config as JSON, or the
//	                                    diagnostics it is refused with, positions included
//
// Control frames are captured by internal/control/fixture's TestCapture.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: oracle-dump <parse|syntax|refs|fmt|edit|eval|config> <input> [outdir]")
		os.Exit(2)
	}
	in := os.Args[2]
	switch os.Args[1] {
	case "parse":
		forFiles(in, parseOne)
	case "syntax":
		forFiles(in, syntaxOne)
	case "refs":
		forFiles(in, refsOne)
	case "fmt":
		fmtAll(in, arg(3))
	case "edit":
		editAll(in, arg(3))
	case "eval":
		evalAll(in)
	case "convert":
		convertAll(in)
	case "config":
		configAll(os.Args[2:])
	default:
		fmt.Fprintln(os.Stderr, "oracle-dump: unknown command", os.Args[1])
		os.Exit(2)
	}
}

func arg(i int) string {
	if len(os.Args) <= i {
		fmt.Fprintln(os.Stderr, "oracle-dump: missing argument", i)
		os.Exit(2)
	}
	return os.Args[i]
}

func files(dir string) []string {
	m, _ := filepath.Glob(filepath.Join(dir, "*.hcl"))
	sort.Strings(m)
	return m
}

func id(path string) string { return strings.TrimSuffix(filepath.Base(path), ".hcl") }

var out = json.NewEncoder(os.Stdout)

func emit(v any) {
	if err := out.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, "oracle-dump:", err)
		os.Exit(1)
	}
}

func forFiles(dir string, f func(src []byte) any) {
	for _, p := range files(dir) {
		src, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "oracle-dump:", err)
			os.Exit(1)
		}
		emit(map[string]any{"id": id(p), "r": f(src)})
	}
}

type pos struct {
	Line int `json:"line"`
	Col  int `json:"col"`
	Byte int `json:"byte"`
}

func rng(r hcl.Range) map[string]any {
	return map[string]any{
		"file":  filepath.Base(r.Filename),
		"start": pos{r.Start.Line, r.Start.Column, r.Start.Byte},
		"end":   pos{r.End.Line, r.End.Column, r.End.Byte},
	}
}

func diagsOf(ds hcl.Diagnostics) []map[string]any {
	out := []map[string]any{}
	for _, d := range ds {
		sev := "error"
		if d.Severity == hcl.DiagWarning {
			sev = "warning"
		}
		m := map[string]any{"sev": sev, "summary": d.Summary, "detail": d.Detail}
		if d.Subject != nil {
			m["at"] = rng(*d.Subject)
		}
		if d.Context != nil {
			m["context"] = rng(*d.Context)
		}
		out = append(out, m)
	}
	return out
}
