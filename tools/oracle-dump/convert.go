package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

type convertCase struct {
	ID   string          `json:"id"`
	Expr string          `json:"expr"`
	Type json.RawMessage `json:"type"`
	// Unify lists types (cty JSON) to unify instead of converting.
	Unify []json.RawMessage `json:"unify"`
	Safe  bool              `json:"safe"`
}

// convertAll converts each case's value to its type with convert.Convert, or unifies its types
// (convert.Unify / UnifyUnsafe), so cty's conversion rules are checked directly.
func convertAll(path string) {
	raw, err := os.ReadFile(path)
	must(err)
	var cases []convertCase
	must(json.Unmarshal(raw, &cases))
	for _, c := range cases {
		emit(convertOne(c))
	}
}

func convertOne(c convertCase) (out map[string]any) {
	defer func() {
		if r := recover(); r != nil {
			out = map[string]any{"id": c.ID, "ok": false, "err": fmt.Sprint("panic: ", r)}
		}
	}()
	if len(c.Unify) > 0 {
		var tys []cty.Type
		for _, t := range c.Unify {
			ty, err := ctyjson.UnmarshalType(t)
			must(err)
			tys = append(tys, ty)
		}
		unify := convert.UnifyUnsafe
		if c.Safe {
			unify = convert.Unify
		}
		got, _ := unify(tys)
		if got == cty.NilType {
			return map[string]any{"id": c.ID, "ok": false}
		}
		js, err := ctyjson.MarshalType(got)
		must(err)
		return map[string]any{"id": c.ID, "ok": true, "type": json.RawMessage(js)}
	}
	f, diags := parseFile([]byte("x = " + c.Expr + "\n"))
	if diags.HasErrors() {
		return map[string]any{"id": c.ID, "ok": false, "err": "parse: " + diags.Error()}
	}
	v, d := f.Body.(*hclsyntax.Body).Attributes["x"].Expr.Value(testContext())
	if d.HasErrors() {
		return map[string]any{"id": c.ID, "ok": false, "err": "eval: " + d.Error()}
	}
	want, err := ctyjson.UnmarshalType(c.Type)
	must(err)
	got, err := convert.Convert(v, want)
	if err != nil {
		return map[string]any{"id": c.ID, "ok": false, "err": err.Error()}
	}
	ty, err := ctyjson.MarshalType(got.Type())
	must(err)
	return map[string]any{"id": c.ID, "ok": true, "value": canon(got), "type": json.RawMessage(ty)}
}
