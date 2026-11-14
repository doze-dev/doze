package main

import (
	"encoding/json"
	"os"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

// typeexprAll reads each expression of a JSON array of {"id","expr"} as a type, the way
// doze reads a variable's `type` (typeexpr.TypeConstraint), and also as an exact type
// (typeexpr.Type): the type as cty's JSON, or the diagnostics.
func typeexprAll(path string) {
	raw, err := os.ReadFile(path)
	must(err)
	var cases []exprCase
	must(json.Unmarshal(raw, &cases))
	for _, c := range cases {
		f, diags := parseFile([]byte("x = " + c.Expr + "\n"))
		if diags.HasErrors() {
			emit(map[string]any{"id": c.ID, "stage": "parse", "diags": diagsOf(diags)})
			continue
		}
		expr := f.Body.(*hclsyntax.Body).Attributes["x"].Expr
		emit(map[string]any{
			"id":         c.ID,
			"constraint": typeResult(typeexpr.TypeConstraint(expr)),
			"exact":      typeResult(typeexpr.Type(expr)),
		})
	}
}

func typeResult(t cty.Type, diags hcl.Diagnostics) map[string]any {
	if diags.HasErrors() {
		return map[string]any{"ok": false, "diags": diagsOf(diags)}
	}
	j, err := ctyjson.MarshalType(t)
	must(err)
	return map[string]any{"ok": true, "type": json.RawMessage(j)}
}
