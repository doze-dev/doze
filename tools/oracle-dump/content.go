package main

import (
	"encoding/json"
	"os"
	"sort"

	"github.com/hashicorp/hcl/v2"
)

type contentCase struct {
	ID     string   `json:"id"`
	Srcs   []string `json:"srcs"` // more than one: hcl.MergeFiles
	Mode   string   `json:"mode"` // content, partial, just
	Attrs  [][2]any `json:"attrs"`
	Blocks [][2]any `json:"blocks"`
}

// contentAll reads bodies against a schema (Body.Content / PartialContent /
// JustAttributes, merged when there are several sources), then each block's body
// with the same schema once more (nested reads): attribute names, blocks, diagnostics.
func contentAll(path string) {
	raw, err := os.ReadFile(path)
	must(err)
	var cases []contentCase
	must(json.Unmarshal(raw, &cases))
	for _, c := range cases {
		var files []*hcl.File
		var bad hcl.Diagnostics
		for i, s := range c.Srcs {
			f, d := parseFileNamed([]byte(s), "f"+string(rune('0'+i))+".hcl")
			bad = append(bad, d...)
			files = append(files, f)
		}
		if bad.HasErrors() {
			emit(map[string]any{"id": c.ID, "parse": false})
			continue
		}
		schema := &hcl.BodySchema{}
		for _, a := range c.Attrs {
			schema.Attributes = append(schema.Attributes, hcl.AttributeSchema{Name: a[0].(string), Required: a[1].(bool)})
		}
		for _, b := range c.Blocks {
			var labels []string
			for _, l := range b[1].([]any) {
				labels = append(labels, l.(string))
			}
			schema.Blocks = append(schema.Blocks, hcl.BlockHeaderSchema{Type: b[0].(string), LabelNames: labels})
		}
		var body hcl.Body
		if len(files) == 1 {
			body = files[0].Body
		} else {
			body = hcl.MergeFiles(files)
		}
		out := map[string]any{"id": c.ID}
		out["top"] = readBody(body, c.Mode, schema, true)
		emit(out)
	}
}

func readBody(body hcl.Body, mode string, schema *hcl.BodySchema, nest bool) map[string]any {
	var attrs hcl.Attributes
	var blocks hcl.Blocks
	var diags hcl.Diagnostics
	out := map[string]any{}
	switch mode {
	case "just":
		attrs, diags = body.JustAttributes()
	case "partial":
		var c *hcl.BodyContent
		var rest hcl.Body
		c, rest, diags = body.PartialContent(schema)
		attrs, blocks = c.Attributes, c.Blocks
		ra, rd := rest.JustAttributes()
		out["rest"] = sortedAttrNames(ra)
		out["rest_diags"] = sortDiags(diagsOf(rd))
	default:
		var c *hcl.BodyContent
		c, diags = body.Content(schema)
		attrs, blocks = c.Attributes, c.Blocks
	}
	out["attrs"] = sortedAttrNames(attrs)
	out["missing"] = rng(body.MissingItemRange())
	bl := []any{}
	for _, b := range blocks {
		entry := map[string]any{"type": b.Type, "labels": b.Labels}
		if nest {
			entry["body"] = readBody(b.Body, "content", schema, false)
		}
		bl = append(bl, entry)
	}
	out["blocks"] = bl
	out["diags"] = sortDiags(diagsOf(diags))
	return out
}

func sortedAttrNames(a hcl.Attributes) []string {
	names := []string{}
	for n := range a {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// sortDiags orders diagnostics by position then summary: hcl reports some from map
// iteration, in random order.
func sortDiags(ds []map[string]any) []map[string]any {
	key := func(d map[string]any) string {
		at, _ := json.Marshal(d["at"])
		return string(at) + d["summary"].(string) + d["detail"].(string)
	}
	sort.SliceStable(ds, func(i, j int) bool { return key(ds[i]) < key(ds[j]) })
	return ds
}
