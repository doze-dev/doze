package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"

	"github.com/doze-dev/doze/internal/config"
)

type exprCase struct {
	ID   string `json:"id"`
	Expr string `json:"expr"`
}

// evalAll evaluates each expression as the value of `x = <expr>` (so heredocs
// and multi-line expressions parse as in a doze.hcl) against a fixed context
// and doze.hcl's function table.
func evalAll(path string) {
	raw, err := os.ReadFile(path)
	must(err)
	var cases []exprCase
	must(json.Unmarshal(raw, &cases))
	ctx := testContext()
	for _, c := range cases {
		emit(evalOne(c, ctx))
	}
}

func evalOne(c exprCase, ctx *hcl.EvalContext) map[string]any {
	f, diags := parseFile([]byte("x = " + c.Expr + "\n"))
	if diags.HasErrors() {
		return map[string]any{"id": c.ID, "ok": false, "stage": "parse", "diags": diagsOf(diags)}
	}
	attr := f.Body.(*hclsyntax.Body).Attributes["x"]
	v, d := attr.Expr.Value(ctx)
	if d.HasErrors() {
		return map[string]any{"id": c.ID, "ok": false, "stage": "eval", "diags": diagsOf(d)}
	}
	ty, err := ctyjson.MarshalType(v.Type())
	must(err)
	return map[string]any{"id": c.ID, "ok": true, "value": canon(v), "type": json.RawMessage(ty)}
}

// canon renders a cty value as JSON with numbers as exact decimal strings and
// sets and maps in cty's order, so two implementations compare as text.
func canon(v cty.Value) any {
	if !v.IsKnown() {
		return map[string]any{"unknown": true}
	}
	if v.IsNull() {
		return nil
	}
	t := v.Type()
	switch {
	case t == cty.String:
		return v.AsString()
	case t == cty.Bool:
		return v.True()
	case t == cty.Number:
		return map[string]any{"num": v.AsBigFloat().Text('f', -1)}
	case t.IsListType() || t.IsTupleType() || t.IsSetType():
		out := []any{}
		for it := v.ElementIterator(); it.Next(); {
			_, e := it.Element()
			out = append(out, canon(e))
		}
		return out
	case t.IsMapType() || t.IsObjectType():
		// A JSON object loses order; a list of pairs keeps cty's.
		out := [][2]any{}
		for it := v.ElementIterator(); it.Next(); {
			k, e := it.Element()
			out = append(out, [2]any{k.AsString(), canon(e)})
		}
		return map[string]any{"obj": out}
	case t == cty.DynamicPseudoType:
		return map[string]any{"dynamic": true}
	}
	return fmt.Sprint(v.GoString())
}

// testContext is the context every eval case sees. Host numbers are parsed
// at full precision (cty.NumberIntVal would make 64-bit floats).
func testContext() *hcl.EvalContext {
	n := cty.MustParseNumberVal
	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"var": cty.ObjectVal(map[string]cty.Value{
				"name":  cty.StringVal("shop"),
				"port":  n("8080"),
				"tags":  cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b"), cty.StringVal("c")}),
				"env":   cty.MapVal(map[string]cty.Value{"A": cty.StringVal("1"), "B": cty.StringVal("2")}),
				"flag":  cty.True,
				"none":  cty.NullVal(cty.String),
				"set":   cty.SetVal([]cty.Value{cty.StringVal("b"), cty.StringVal("a")}),
				"users": cty.TupleVal([]cty.Value{cty.ObjectVal(map[string]cty.Value{"name": cty.StringVal("ann"), "age": n("31")}), cty.ObjectVal(map[string]cty.Value{"name": cty.StringVal("bo"), "age": n("27")})}),
				"later": cty.UnknownVal(cty.String),
			}),
			"local": cty.ObjectVal(map[string]cty.Value{"base": cty.StringVal("/srv"), "n": n("3")}),
		},
		Functions: config.OracleFunctions(),
	}
}
