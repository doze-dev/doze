// Load/Parse entry points and the buildConfig pipeline that turns (possibly
// merged) HCL bodies into a validated Config.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// loader carries the per-load decode state: the HCL parser (for positioned
// diagnostics) and the caller's module-integration hooks (nil for pure parse).
type loader struct {
	parser *hclparse.Parser
	hooks  *Hooks
}

// Load reads and validates the doze configuration at path, with no variable
// overrides. See LoadWithVars.
func Load(path string, hooks *Hooks) (*Config, error) { return LoadWithVars(path, nil, hooks) }

// LoadWithVars reads and validates the doze configuration at path. path may be a
// single file (e.g. doze.hcl) — in which case every sibling *.doze.hcl is merged
// in — or a directory, in which case all of its *.hcl files are merged. cliVars
// are --var overrides (highest precedence). Variable values also come from
// DOZE_VAR_<name> env vars and sibling *.auto.doze.vars files. hooks integrates
// the module fetcher into decode; nil means pure parse.
func LoadWithVars(path string, cliVars map[string]string, hooks *Hooks) (*Config, error) {
	files, primary, err := gatherConfigFiles(path)
	if err != nil {
		return nil, err
	}
	parser := hclparse.NewParser()
	hclFiles := make([]*hcl.File, 0, len(files))
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		src, err = rewriteUseBlocks(src, f)
		if err != nil {
			return nil, err
		}
		hf, diags := parser.ParseHCL(src, f)
		if diags.HasErrors() {
			return nil, diagError(parser, diags)
		}
		hclFiles = append(hclFiles, hf)
	}
	autoVars, err := loadAutoVars(configDirOf(path))
	if err != nil {
		return nil, err
	}
	l := &loader{parser: parser, hooks: hooks}
	cfg, err := l.buildConfig(hcl.MergeFiles(hclFiles), primary, &varInputs{cli: cliVars, auto: autoVars}, engineBlockTypes(hclFiles))
	if err != nil {
		return nil, err
	}
	if err := cfg.validatePorts(); err != nil {
		return nil, err
	}
	if err := cfg.validateDomains(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// configDirOf returns the directory that holds the config (and its sibling
// *.auto.doze.vars files): path itself if it is a directory, else its parent.
func configDirOf(path string) string {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

// gatherConfigFiles resolves the ordered set of HCL files to merge, and the
// "primary" path used for diagnostics and the project slug. doze.hcl is always
// first so its root settings are authoritative; sibling *.doze.hcl are appended sorted.
func gatherConfigFiles(path string) (files []string, primary string, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", err
	}
	if info.IsDir() {
		matches, _ := filepath.Glob(filepath.Join(path, "*.hcl"))
		sort.Strings(matches)
		if len(matches) == 0 {
			return nil, "", fmt.Errorf("no .hcl files found in %s", path)
		}
		return matches, path, nil
	}
	// The anchor file is authoritative for root settings; every sibling
	// *.doze.hcl is auto-merged (sorted), so config can be split by concern
	// (databases.doze.hcl, aws.doze.hcl, …) without an includes directive.
	files = []string{path}
	extra, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.doze.hcl"))
	sort.Strings(extra)
	for _, f := range extra {
		if f != path { // the anchor may itself be a *.doze.hcl; don't double-load it
			files = append(files, f)
		}
	}
	return files, path, nil
}

// Parse validates HCL source bytes. filename is used only for diagnostics.
// Engine drivers must already be registered (cmd/doze blank-imports them).
// hooks integrates the module fetcher into decode; nil means pure parse.
func Parse(src []byte, filename string, hooks *Hooks) (*Config, error) {
	src, err := rewriteUseBlocks(src, filename)
	if err != nil {
		return nil, err
	}
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(src, filename)
	if diags.HasErrors() {
		return nil, diagError(parser, diags)
	}
	l := &loader{parser: parser, hooks: hooks}
	return l.buildConfig(file.Body, filename, nil, engineBlockTypes([]*hcl.File{file}))
}

// buildConfig validates a (possibly merged) HCL body into a Config. engineTypes is
// the set of engine block types declared in the source (every non-reserved labeled
// block); each is accepted into the schema and validated to a real driver later.
func (l *loader) buildConfig(body hcl.Body, primaryPath string, inputs *varInputs, engineTypes []string) (*Config, error) {
	parser := l.parser
	// Accept every declared engine block type — config can't enumerate engines
	// (they're out-of-process modules), so it trusts the type and resolves it later.
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "listen"}, {Name: "home"}, {Name: "data_dir"}, {Name: "name"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "defaults"},
			{Type: "tls"},
			{Type: "modules"},
			{Type: "variable", LabelNames: []string{"name"}},
			{Type: "locals"},
			{Type: "output", LabelNames: []string{"name"}},
		},
	}
	for _, t := range engineTypes {
		schema.Blocks = append(schema.Blocks, hcl.BlockHeaderSchema{Type: t, LabelNames: []string{"name"}})
	}

	content, diags := body.Content(schema)
	if diags.HasErrors() {
		return nil, diagError(parser, diags)
	}

	cfg := &Config{
		path:     primaryPath,
		index:    map[string]*InstanceDecl{},
		Listen:   DefaultListen,
		Defaults: Defaults{IdleTimeout: DefaultIdleTimeout},
	}

	// Classify the top-level blocks. variable/locals/output are resolved before
	// the resources so that var.*/local.* are available everywhere.
	var varBlocks, localsBlocks, outputBlocks, instanceBlocks []*hcl.Block
	seenDefaults, seenTLS, seenModules := false, false, false
	var defaultsBlock, tlsBlock, modulesBlock *hcl.Block
	for _, block := range content.Blocks {
		switch block.Type {
		case "defaults":
			if seenDefaults {
				return nil, posErr(parser, block.DefRange, "duplicate defaults block", "only one defaults block is allowed across all config files")
			}
			seenDefaults, defaultsBlock = true, block
		case "tls":
			if seenTLS {
				return nil, posErr(parser, block.DefRange, "duplicate tls block", "only one tls block is allowed across all config files")
			}
			seenTLS, tlsBlock = true, block
		case "modules":
			if seenModules {
				return nil, posErr(parser, block.DefRange, "duplicate modules block", "only one modules block is allowed across all config files")
			}
			seenModules, modulesBlock = true, block
		case "variable":
			varBlocks = append(varBlocks, block)
		case "locals":
			localsBlocks = append(localsBlocks, block)
		case "output":
			outputBlocks = append(outputBlocks, block)
		default:
			instanceBlocks = append(instanceBlocks, block)
		}
	}

	// Build the evaluation context: functions, then variables, then locals.
	ctx := &hcl.EvalContext{Variables: map[string]cty.Value{}, Functions: stdlibFunctions()}
	varObj, err := resolveVariables(parser, varBlocks, inputs, ctx)
	if err != nil {
		return nil, err
	}
	ctx.Variables["var"] = cty.ObjectVal(emptyIfNil(varObj))
	if err := evaluateLocals(parser, localsBlocks, ctx); err != nil {
		return nil, err
	}

	// Root attributes and defaults/tls — may now reference var.*/local.*.
	for name, attr := range content.Attributes {
		var dst *string
		switch name {
		case "listen":
			dst = &cfg.Listen
		case "home":
			dst = &cfg.Home
		case "data_dir":
			dst = &cfg.DataDir
		case "name":
			dst = &cfg.StackName
		}
		if diags := gohcl.DecodeExpression(attr.Expr, ctx, dst); diags.HasErrors() {
			return nil, diagError(parser, diags)
		}
	}
	if err := cfg.resolveHome(); err != nil {
		return nil, err
	}
	if defaultsBlock != nil {
		if err := cfg.decodeDefaults(parser, defaultsBlock, ctx); err != nil {
			return nil, err
		}
	}
	if tlsBlock != nil {
		if err := cfg.decodeTLS(parser, tlsBlock, ctx); err != nil {
			return nil, err
		}
	}
	// Decode + apply modules{} BEFORE instance expansion below — expanding an
	// instance block resolves its driver (engine.Lookup), which for a plugin engine
	// fetches the module, so the mirror/enable/version pins must already be set.
	if modulesBlock != nil {
		if err := cfg.decodeModules(parser, modulesBlock, ctx); err != nil {
			return nil, err
		}
	}
	l.hooks.configureModules(cfg.Modules)

	// Instance shells (engine-agnostic fields), then the reference graph + driver
	// bodies in dependency order, then outputs (which may reference everything).
	declRanges := map[string]hcl.Range{}
	var pending []*pendingInstance
	for _, block := range instanceBlocks {
		ps, err := l.expandInstanceBlock(cfg, block, declRanges, ctx)
		if err != nil {
			return nil, err
		}
		pending = append(pending, ps...)
	}
	if err := l.evaluate(cfg, pending, ctx); err != nil {
		return nil, err
	}
	// Post-decode engine-support pass: the FIRST block of a type drives module
	// selection, so a later block declaring a version the resolved module can't
	// serve is only caught here — with the block's own position and an
	// actionable upgrade command.
	for _, p := range pending {
		if err := l.hooks.checkSupport(p.decl.Type, string(p.decl.Version)); err != nil {
			return nil, posErr(parser, p.defRange, err.Error(), "")
		}
	}
	if err := cfg.evaluateOutputs(parser, outputBlocks, ctx); err != nil {
		return nil, err
	}
	return cfg, nil
}

func emptyIfNil(m map[string]cty.Value) map[string]cty.Value {
	if m == nil {
		return map[string]cty.Value{}
	}
	return m
}

// checkBlockTypes reports the first unknown top-level block type with a friendly,
// positioned diagnostic (with a "did you mean" suggestion) — better than HCL's
// generic "Unsupported block type". Operates on native-syntax bodies.
// reservedBlocks are the non-engine top-level block types. Every other labeled
// top-level block is an engine instance — its type isn't validated here (engines
// are out-of-process modules config can't enumerate), only at driver resolution.
var reservedBlocks = map[string]bool{
	"defaults": true, "tls": true, "modules": true,
	"variable": true, "locals": true, "output": true,
}

// engineBlockTypes returns the distinct engine block types declared across the
// files (every top-level labeled block that isn't a reserved keyword). They seed
// the decode schema and reference resolution; whether a type actually resolves to
// a driver (in-tree or a fetched module) is checked when the instance is built.
func engineBlockTypes(files []*hcl.File) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		body, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for _, blk := range body.Blocks {
			if reservedBlocks[blk.Type] || seen[blk.Type] {
				continue
			}
			seen[blk.Type] = true
			out = append(out, blk.Type)
		}
	}
	sort.Strings(out)
	return out
}
