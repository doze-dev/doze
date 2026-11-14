package main

import (
	"encoding/json"
	"os"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// srcCase is one generated source for the lex and check modes, which take a JSON
// array of {"id","src"} rather than a directory, so mutation corpora need no files.
type srcCase struct {
	ID  string `json:"id"`
	Src string `json:"src"`
}

func readCases(path string) []srcCase {
	raw, err := os.ReadFile(path)
	must(err)
	var cases []srcCase
	must(json.Unmarshal(raw, &cases))
	return cases
}

// lexAll writes hclsyntax's token stream for each source: type, bytes and
// positions, then the lexer's own diagnostics (invalid and bad UTF-8 tokens, …).
func lexAll(path string) {
	for _, c := range readCases(path) {
		toks, diags := hclsyntax.LexConfig([]byte(c.Src), "x.hcl", hcl.Pos{Line: 1, Column: 1})
		out := make([][]any, 0, len(toks))
		for _, t := range toks {
			r := t.Range
			out = append(out, []any{t.Type.String(), r.Start.Byte, r.End.Byte, r.Start.Line, r.Start.Column, r.End.Line, r.End.Column})
		}
		emit(map[string]any{"id": c.ID, "tokens": out, "diags": diagsOf(diags)})
	}
}

// checkAll parses each source as a config file: accepted or not, and every
// diagnostic in Go's order with positions.
func checkAll(path string) {
	for _, c := range readCases(path) {
		emit(map[string]any{"id": c.ID, "r": parseOneSafe([]byte(c.Src))})
	}
}

func parseOneSafe(src []byte) (out any) {
	defer func() {
		if r := recover(); r != nil {
			out = map[string]any{"ok": false, "panic": true}
		}
	}()
	return parseOne(src)
}
