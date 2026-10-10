package config

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

// ShallowDecl is one module or app block header from a shallow load: the kind,
// engine type, name, and declared engine version — nothing that needs a driver.
type ShallowDecl struct {
	Kind    string // KindModule or KindApp
	Type    string // the engine type; AppEngine for an app
	Name    string
	Version string // "" when unset or not statically evaluable (a var reference)
}

// ShallowConfig is the driver-free slice of a project config.
type ShallowConfig struct {
	Decls []ShallowDecl
	path  string
}

// Path returns the primary config path the shallow load anchored on.
func (c *ShallowConfig) Path() string { return c.path }

// LoadShallow reads just the module and app block headers (type, name, declared version) from the config at path — no driver
// lookup, no plugin launch, no body decode, no hooks. It exists for commands that
// must run when the full load can't: `doze modules upgrade` fixes the exact
// protocol/engine-support gates that fail a full load, so it cannot depend on
// one succeeding. Versions referencing variables are returned empty (the module
// requirement is simply not narrowed by them here).
func LoadShallow(path string) (*ShallowConfig, error) {
	files, primary, err := gatherConfigFiles(path)
	if err != nil {
		return nil, err
	}
	parser := hclparse.NewParser()
	hclFiles := make([]*hcl.File, 0, len(files))
	for _, f := range files {
		hf, diags := parser.ParseHCLFile(f)
		if diags.HasErrors() {
			return nil, diagError(parser, diags)
		}
		hclFiles = append(hclFiles, hf)
	}
	if err := checkOldForms(parser, hclFiles); err != nil {
		return nil, err
	}
	merged := hcl.MergeFiles(hclFiles)

	out := &ShallowConfig{path: primary}
	schema := &hcl.BodySchema{Blocks: []hcl.BlockHeaderSchema{
		{Type: KindModule, LabelNames: []string{"type", "name"}},
		{Type: KindApp, LabelNames: []string{"name"}},
	}}
	content, _, _ := merged.PartialContent(schema)
	for _, block := range content.Blocks {
		decl := ShallowDecl{Kind: KindApp, Type: AppEngine, Name: block.Labels[0]}
		if block.Type == KindModule {
			decl = ShallowDecl{Kind: KindModule, Type: block.Labels[0], Name: block.Labels[1]}
		}
		attrs, _, _ := block.Body.PartialContent(&hcl.BodySchema{Attributes: []hcl.AttributeSchema{{Name: "version"}}})
		if attr, ok := attrs.Attributes["version"]; ok {
			// Static evaluation only — var/local references yield "" rather than
			// dragging the whole evaluator in.
			if v, diags := attr.Expr.Value(nil); !diags.HasErrors() && !v.IsNull() {
				if s, err := convert.Convert(v, cty.String); err == nil && !s.IsNull() {
					decl.Version = s.AsString()
				}
			}
		}
		out.Decls = append(out.Decls, decl)
	}
	return out, nil
}
