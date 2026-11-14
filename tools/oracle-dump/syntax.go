package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"

	"github.com/doze-dev/doze/internal/config"
)

func parseFile(src []byte) (*hcl.File, hcl.Diagnostics) {
	return hclsyntax.ParseConfig(src, "x.hcl", hcl.Pos{Line: 1, Column: 1, Byte: 0})
}

func parseOne(src []byte) any {
	_, diags := parseFile(src)
	return map[string]any{"ok": !diags.HasErrors(), "diags": diagsOf(diags)}
}

func fmtAll(dir, outDir string) {
	must(os.MkdirAll(outDir, 0o755))
	for _, p := range files(dir) {
		src, err := os.ReadFile(p)
		must(err)
		if _, d := parseFile(src); d.HasErrors() {
			continue
		}
		must(os.WriteFile(filepath.Join(outDir, id(p)+".hcl"), hclwrite.Format(src), 0o644))
	}
}

func bodyShape(b *hclsyntax.Body) map[string]any {
	attrs := []map[string]any{}
	for _, name := range sortedKeys(b.Attributes) {
		a := b.Attributes[name]
		attrs = append(attrs, map[string]any{"name": name, "line": a.SrcRange.Start.Line})
	}
	blocks := []map[string]any{}
	for _, bl := range b.Blocks {
		blocks = append(blocks, map[string]any{"type": bl.Type, "labels": bl.Labels, "line": bl.TypeRange.Start.Line, "body": bodyShape(bl.Body)})
	}
	return map[string]any{"attrs": attrs, "blocks": blocks}
}

func sortedKeys(m hclsyntax.Attributes) []string {
	k := make([]string, 0, len(m))
	for n := range m {
		k = append(k, n)
	}
	sort.Strings(k)
	return k
}

func syntaxOne(src []byte) any {
	f, diags := parseFile(src)
	if diags.HasErrors() {
		return nil
	}
	return bodyShape(f.Body.(*hclsyntax.Body))
}

func collectRefs(b *hclsyntax.Body, out *[]map[string]any) {
	for _, name := range sortedKeys(b.Attributes) {
		a := b.Attributes[name]
		for _, t := range a.Expr.Variables() {
			var parts []string
			for _, s := range t {
				switch s := s.(type) {
				case hcl.TraverseRoot:
					parts = append(parts, s.Name)
				case hcl.TraverseAttr:
					parts = append(parts, s.Name)
				case hcl.TraverseIndex:
					parts = append(parts, "["+s.Key.GoString()+"]")
				case hcl.TraverseSplat:
					parts = append(parts, "[*]")
				}
			}
			r := t.SourceRange()
			*out = append(*out, map[string]any{"attr": name, "ref": strings.Join(parts, "."), "line": r.Start.Line, "col": r.Start.Column})
		}
	}
	for _, bl := range b.Blocks {
		collectRefs(bl.Body, out)
	}
}

func refsOne(src []byte) any {
	f, diags := parseFile(src)
	if diags.HasErrors() {
		return nil
	}
	out := []map[string]any{}
	collectRefs(f.Body.(*hclsyntax.Body), &out)
	return out
}

// appendedBlock is what the edit check appends, as `doze add` does through
// internal/config's Edit (format the snippet, a blank line, then the block).
const appendedBlock = "app \"extra\" {\n  run = \"go run ./cmd/extra\"\n  port=9090\n}\n"

func editAll(dir, outDir string) {
	must(os.MkdirAll(outDir, 0o755))
	for _, p := range files(dir) {
		src, err := os.ReadFile(p)
		must(err)
		e, err := config.OpenEdit(src)
		if err != nil {
			continue
		}
		if err := e.AppendBlock(appendedBlock); err != nil {
			continue
		}
		must(os.WriteFile(filepath.Join(outDir, id(p)+".hcl"), e.Bytes(), 0o644))
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
