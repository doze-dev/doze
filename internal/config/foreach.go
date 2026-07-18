package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"

	"github.com/doze-dev/doze-sdk/engine"
)

// stamp is one expansion of an instance block: a name suffix and the each/count
// variables to expose while decoding it. A plain block has a single empty stamp.
type stamp struct {
	suffix string               // "" for a non-meta block; the for_each key or count index
	vars   map[string]cty.Value // each.* / count.* for the child context; nil if none
}

// name returns the stamped instance name: the label itself for a plain block, or
// label_<suffix> for a for_each/count expansion.
func (s stamp) name(label string) string {
	if s.suffix == "" {
		return label
	}
	return label + "_" + s.suffix
}

// instanceStamps resolves an instance block's count/for_each into the set of
// stamps to produce (a single empty stamp when neither is set).
func (cfg *Config) instanceStamps(parser *hclparse.Parser, block *hcl.Block, countAttr, forEachAttr *hcl.Attribute, hasCount, hasForEach bool, ctx *hcl.EvalContext) ([]stamp, error) {
	switch {
	case hasForEach:
		return forEachStamps(parser, block, forEachAttr, ctx)
	case hasCount:
		return countStamps(parser, block, countAttr, ctx)
	default:
		return []stamp{{}}, nil
	}
}

// forEachStamps stamps one instance per element of a map (key→value) or a set of
// strings (key == value), sorted by key for deterministic port assignment.
func forEachStamps(parser *hclparse.Parser, block *hcl.Block, attr *hcl.Attribute, ctx *hcl.EvalContext) ([]stamp, error) {
	val, diags := attr.Expr.Value(ctx)
	if diags.HasErrors() {
		return nil, diagError(parser, diags)
	}
	if val.IsNull() || !val.IsKnown() {
		return nil, posErr(parser, attr.Range, "invalid for_each", "value must be a known map or set")
	}
	mkEach := func(k, v cty.Value) map[string]cty.Value {
		return map[string]cty.Value{"each": cty.ObjectVal(map[string]cty.Value{"key": k, "value": v})}
	}
	var stamps []stamp
	ty := val.Type()
	switch {
	case ty.IsMapType() || ty.IsObjectType():
		it := val.ElementIterator()
		for it.Next() {
			k, v := it.Element()
			key := k.AsString()
			stamps = append(stamps, stamp{suffix: sanitizeName(key), vars: mkEach(cty.StringVal(key), v)})
		}
	case ty.IsSetType() || ty.IsListType() || ty.IsTupleType():
		it := val.ElementIterator()
		for it.Next() {
			_, e := it.Element()
			if e.Type() != cty.String {
				return nil, posErr(parser, attr.Range, "invalid for_each", "set/list elements must be strings")
			}
			stamps = append(stamps, stamp{suffix: sanitizeName(e.AsString()), vars: mkEach(e, e)})
		}
	default:
		return nil, posErr(parser, attr.Range, "invalid for_each", "value must be a map or a set of strings")
	}
	sort.Slice(stamps, func(i, j int) bool { return stamps[i].suffix < stamps[j].suffix })
	return stamps, nil
}

// countStamps stamps N instances (suffix 0..N-1), exposing count.index.
func countStamps(parser *hclparse.Parser, block *hcl.Block, attr *hcl.Attribute, ctx *hcl.EvalContext) ([]stamp, error) {
	val, diags := attr.Expr.Value(ctx)
	if diags.HasErrors() {
		return nil, diagError(parser, diags)
	}
	if val.IsNull() || !val.IsKnown() || val.Type() != cty.Number {
		return nil, posErr(parser, attr.Range, "invalid count", "value must be a non-negative whole number")
	}
	n64, _ := val.AsBigFloat().Int64()
	if n64 < 0 {
		return nil, posErr(parser, attr.Range, "invalid count", "must not be negative")
	}
	stamps := make([]stamp, 0, n64)
	for i := int64(0); i < n64; i++ {
		stamps = append(stamps, stamp{
			suffix: strconv.FormatInt(i, 10),
			vars:   map[string]cty.Value{"count": cty.ObjectVal(map[string]cty.Value{"index": cty.NumberIntVal(i)})},
		})
	}
	return stamps, nil
}

// parseDependsOn decodes a depends_on meta-arg — a map of an instance address to
// a readiness condition: depends_on = { "postgres.app" = "healthy" }. The key may
// be a bare instance name or "<type>.<name>"; the value must be "healthy" or
// "started". It is groundwork for the future process engine; for current engines
// the runtime waits for Healthy regardless. Returns instance name → condition.
func parseDependsOn(parser *hclparse.Parser, attr *hcl.Attribute, ctx *hcl.EvalContext) (map[string]engine.Condition, error) {
	val, diags := attr.Expr.Value(ctx)
	if diags.HasErrors() {
		return nil, diagError(parser, diags)
	}
	if val.IsNull() {
		return nil, nil
	}
	ty := val.Type()
	if !ty.IsObjectType() && !ty.IsMapType() {
		return nil, posErr(parser, attr.Range, "invalid depends_on",
			`expected a map of address to condition, e.g. { "postgres.app" = "healthy" }`)
	}
	out := map[string]engine.Condition{}
	it := val.ElementIterator()
	for it.Next() {
		k, v := it.Element()
		if v.Type() != cty.String {
			return nil, posErr(parser, attr.Range, "invalid depends_on", "condition must be a string")
		}
		cond := engine.Condition(v.AsString())
		if cond != engine.Healthy && cond != engine.Started && cond != engine.Lazy {
			return nil, posErr(parser, attr.Range, "invalid depends_on condition",
				fmt.Sprintf("%q is not a condition (want %q, %q or %q)", v.AsString(), engine.Healthy, engine.Started, engine.Lazy))
		}
		out[dependsOnName(k.AsString())] = cond
	}
	return out, nil
}

// dependsOnName extracts the instance name from a depends_on key, accepting both
// a bare name ("app") and a "<type>.<name>" address ("postgres.app").
func dependsOnName(key string) string {
	if _, name, ok := strings.Cut(key, "."); ok {
		return name
	}
	return key
}

// sanitizeName makes a for_each key safe for an instance name (and the paths /
// env vars derived from it): everything outside [A-Za-z0-9_-] becomes '_'.
func sanitizeName(key string) string {
	var b strings.Builder
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}
