package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

// hclsyntaxParseExpr parses a standalone HCL expression from a string override
// (e.g. a --var value for a list/map-typed variable).
func hclsyntaxParseExpr(s string) (hcl.Expression, hcl.Diagnostics) {
	return hclsyntax.ParseExpression([]byte(s), "<override>", hcl.Pos{Line: 1, Column: 1})
}

// EnvVarPrefix is the environment-variable prefix for variable overrides:
// DOZE_VAR_<name> sets variable "name".
const EnvVarPrefix = "DOZE_VAR_"

// AutoVarsSuffix is the suffix of files auto-loaded for variable values, the way
// Terraform auto-loads *.auto.tfvars.
const AutoVarsSuffix = ".auto.doze.vars"

// varInputs are the externally-supplied variable values, by precedence source.
// Resolution order (highest first): cli (--var) > DOZE_VAR_<name> env > auto
// (*.auto.doze.vars files) > the variable's default.
type varInputs struct {
	cli  map[string]string    // --var name=value
	auto map[string]cty.Value // *.auto.doze.vars file assignments
}

type hclVariable struct {
	Type        hcl.Expression `hcl:"type,optional"`
	Default     hcl.Expression `hcl:"default,optional"`
	Description string         `hcl:"description,optional"`
	Sensitive   bool           `hcl:"sensitive,optional"`
}

// resolveVariables decodes every `variable` block and resolves its value from the
// override sources (or its default), returning the cty object for ctx.var.
func resolveVariables(parser *hclparse.Parser, blocks []*hcl.Block, in *varInputs, ctx *hcl.EvalContext) (map[string]cty.Value, error) {
	if in == nil {
		in = &varInputs{}
	}
	vars := map[string]cty.Value{}
	seen := map[string]hcl.Range{}
	for _, block := range blocks {
		name := block.Labels[0]
		if first, dup := seen[name]; dup {
			return nil, posErr(parser, block.DefRange,
				fmt.Sprintf("variable %q is already declared", name), "first declared at "+first.String())
		}
		seen[name] = block.DefRange

		var v hclVariable
		if diags := gohcl.DecodeBody(block.Body, ctx, &v); diags.HasErrors() {
			return nil, diagError(parser, diags)
		}
		// Optional type constraint (string, number, bool, list(string), …).
		wantType := cty.DynamicPseudoType
		if v.Type != nil && !exprIsNull(v.Type) {
			t, diags := typeexpr.TypeConstraint(v.Type)
			if diags.HasErrors() {
				return nil, diagError(parser, diags)
			}
			wantType = t
		}

		val, err := resolveOneVar(parser, name, &v, wantType, in, ctx, block.DefRange)
		if err != nil {
			return nil, err
		}
		vars[name] = val
	}
	return vars, nil
}

// resolveOneVar applies the precedence chain for a single variable.
func resolveOneVar(parser *hclparse.Parser, name string, v *hclVariable, wantType cty.Type, in *varInputs, ctx *hcl.EvalContext, rng hcl.Range) (cty.Value, error) {
	convertTo := func(val cty.Value) (cty.Value, error) {
		if wantType == cty.DynamicPseudoType {
			return val, nil
		}
		out, err := convert.Convert(val, wantType)
		if err != nil {
			return cty.NilVal, posErr(parser, rng, fmt.Sprintf("variable %q: invalid value", name), err.Error())
		}
		return out, nil
	}

	// 1. --var (a string, parsed against the declared type).
	if s, ok := in.cli[name]; ok {
		val, err := parseStringValue(parser, name, s, wantType, "--var", rng)
		if err != nil {
			return cty.NilVal, err
		}
		return convertTo(val)
	}
	// 2. DOZE_VAR_<name> env (also a string).
	if s, ok := os.LookupEnv(EnvVarPrefix + name); ok {
		val, err := parseStringValue(parser, name, s, wantType, EnvVarPrefix+name, rng)
		if err != nil {
			return cty.NilVal, err
		}
		return convertTo(val)
	}
	// 3. *.auto.doze.vars file assignment (already a typed cty value).
	if val, ok := in.auto[name]; ok {
		return convertTo(val)
	}
	// 4. default.
	if v.Default != nil && !exprIsNull(v.Default) {
		val, diags := v.Default.Value(ctx)
		if diags.HasErrors() {
			return cty.NilVal, diagError(parser, diags)
		}
		return convertTo(val)
	}
	return cty.NilVal, posErr(parser, rng,
		fmt.Sprintf("no value for required variable %q", name),
		fmt.Sprintf("set it with --var %s=…, the %s%s env var, a %s file, or a default", name, EnvVarPrefix, name, AutoVarsSuffix))
}

// parseStringValue converts a raw string override into the variable's type. A
// string type is taken verbatim; number/bool are parsed; any other (list/map)
// type is parsed as an HCL expression so e.g. --var 'tags=["a","b"]' works.
func parseStringValue(parser *hclparse.Parser, name, s string, wantType cty.Type, source string, rng hcl.Range) (cty.Value, error) {
	if wantType == cty.String || wantType == cty.DynamicPseudoType {
		return cty.StringVal(s), nil
	}
	if wantType == cty.Number || wantType == cty.Bool {
		v, err := convert.Convert(cty.StringVal(s), wantType)
		if err != nil {
			return cty.NilVal, posErr(parser, rng, fmt.Sprintf("variable %q from %s: invalid value %q", name, source, s), err.Error())
		}
		return v, nil
	}
	expr, diags := hclsyntaxParseExpr(s)
	if diags.HasErrors() {
		return cty.NilVal, posErr(parser, rng, fmt.Sprintf("variable %q from %s: not a valid expression", name, source), diags.Error())
	}
	v, vd := expr.Value(nil)
	if vd.HasErrors() {
		return cty.NilVal, posErr(parser, rng, fmt.Sprintf("variable %q from %s: invalid value", name, source), vd.Error())
	}
	return v, nil
}

// evaluateLocals decodes `locals` blocks and evaluates each assignment, in
// dependency order among the locals, into ctx.local.
func evaluateLocals(parser *hclparse.Parser, blocks []*hcl.Block, ctx *hcl.EvalContext) error {
	// Collect every local assignment across all locals blocks.
	exprs := map[string]hcl.Expression{}
	var order []string
	declRange := map[string]hcl.Range{}
	for _, block := range blocks {
		attrs, diags := block.Body.JustAttributes()
		if diags.HasErrors() {
			return diagError(parser, diags)
		}
		for n, attr := range attrs {
			if _, dup := exprs[n]; dup {
				return posErr(parser, attr.NameRange, fmt.Sprintf("local %q is already declared", n), "first declared at "+declRange[n].String())
			}
			exprs[n] = attr.Expr
			declRange[n] = attr.NameRange
			order = append(order, n)
		}
	}
	if len(exprs) == 0 {
		return nil
	}
	sort.Strings(order) // deterministic; dependency order is resolved below

	locals := map[string]cty.Value{}
	state := map[string]int{} // 0 unvisited, 1 visiting, 2 done
	var resolve func(n string, stack []string) error
	resolve = func(n string, stack []string) error {
		switch state[n] {
		case 2:
			return nil
		case 1:
			return posErr(parser, declRange[n], "cycle between locals", strings.Join(append(stack, n), " → "))
		}
		state[n] = 1
		for _, t := range exprs[n].Variables() {
			if t.RootName() == "local" {
				if dep, ok := traversalName(t); ok && dep != n {
					if _, isLocal := exprs[dep]; isLocal {
						if err := resolve(dep, append(stack, n)); err != nil {
							return err
						}
					}
				}
			}
		}
		ctx.Variables["local"] = cty.ObjectVal(locals)
		val, diags := exprs[n].Value(ctx)
		if diags.HasErrors() {
			return diagError(parser, diags)
		}
		locals[n] = val
		state[n] = 2
		return nil
	}
	for _, n := range order {
		if err := resolve(n, nil); err != nil {
			return err
		}
	}
	ctx.Variables["local"] = cty.ObjectVal(locals)
	return nil
}

// evaluateOutputs decodes `output` blocks and renders each value against the
// final context (variables, locals, and all resource attributes).
func (cfg *Config) evaluateOutputs(parser *hclparse.Parser, blocks []*hcl.Block, ctx *hcl.EvalContext) error {
	cfg.Outputs = map[string]Output{}
	seen := map[string]hcl.Range{}
	for _, block := range blocks {
		name := block.Labels[0]
		if first, dup := seen[name]; dup {
			return posErr(parser, block.DefRange, fmt.Sprintf("output %q is already declared", name), "first declared at "+first.String())
		}
		seen[name] = block.DefRange
		var o struct {
			Value       hcl.Expression `hcl:"value"`
			Description string         `hcl:"description,optional"`
			Sensitive   bool           `hcl:"sensitive,optional"`
		}
		if diags := gohcl.DecodeBody(block.Body, ctx, &o); diags.HasErrors() {
			return diagError(parser, diags)
		}
		val, diags := o.Value.Value(ctx)
		if diags.HasErrors() {
			return diagError(parser, diags)
		}
		cfg.Outputs[name] = Output{Name: name, Value: renderValue(val), Description: o.Description, Sensitive: o.Sensitive}
		cfg.OutputOrder = append(cfg.OutputOrder, name)
	}
	return nil
}

// loadAutoVars reads every *.auto.doze.vars file in dir and returns the merged
// name→value assignments (later files win, sorted by name).
func loadAutoVars(dir string) (map[string]cty.Value, error) {
	matches, _ := filepath.Glob(filepath.Join(dir, "*"+AutoVarsSuffix))
	sort.Strings(matches)
	out := map[string]cty.Value{}
	parser := hclparse.NewParser()
	fctx := &hcl.EvalContext{Functions: stdlibFunctions()}
	for _, f := range matches {
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		file, diags := parser.ParseHCL(src, f)
		if diags.HasErrors() {
			return nil, diagError(parser, diags)
		}
		attrs, diags := file.Body.JustAttributes()
		if diags.HasErrors() {
			return nil, diagError(parser, diags)
		}
		for n, attr := range attrs {
			v, vd := attr.Expr.Value(fctx)
			if vd.HasErrors() {
				return nil, diagError(parser, vd)
			}
			out[n] = v
		}
	}
	return out, nil
}

// renderValue turns a resolved cty value into the string doze stores/prints.
func renderValue(v cty.Value) string {
	if v.IsNull() {
		return ""
	}
	if v.Type() == cty.String {
		return v.AsString()
	}
	// Numbers, bools, and collections render via cty's canonical form.
	return v.GoString()
}

func exprIsNull(e hcl.Expression) bool {
	v, diags := e.Value(nil)
	return !diags.HasErrors() && v.IsNull()
}
