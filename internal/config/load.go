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
	"github.com/zclconf/go-cty/cty"
)

// loader carries the per-load decode state: the HCL parser (for positioned
// diagnostics), the parsed files, and the caller's module-integration hooks
// (nil for pure parse).
type loader struct {
	parser *hclparse.Parser
	files  []*hcl.File
	hooks  *Hooks
	// shape is the offline pass: everything about the config that can be
	// checked without resolving a module. No driver is looked up, no module
	// body is decoded, and a module's attributes are stand-ins.
	shape bool
	// pluginSrc caches each file's bytes in the form a plugin decodes.
	pluginSrc map[string][]byte
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
	return load(path, cliVars, &loader{hooks: hooks})
}

// LoadShape runs the offline pass over the configuration at path: block kinds
// and names, the arguments of app, service and task blocks, references that
// point at something declared, and cycles. It resolves no module, so it needs
// neither the network nor the module cache; a module's own body is checked by
// the full load. The Config it returns carries every declaration but no module
// spec, and must not be run.
func LoadShape(path string, cliVars map[string]string) (*Config, error) {
	return load(path, cliVars, &loader{shape: true})
}

func load(path string, cliVars map[string]string, l *loader) (*Config, error) {
	files, primary, err := gatherConfigFiles(path)
	if err != nil {
		return nil, err
	}
	l.parser = hclparse.NewParser()
	for _, f := range files {
		hf, diags := l.parser.ParseHCLFile(f)
		if diags.HasErrors() {
			return nil, diagError(l.parser, diags)
		}
		l.files = append(l.files, hf)
	}
	autoVars, err := loadAutoVars(configDirOf(path))
	if err != nil {
		return nil, err
	}
	cfg, err := l.buildConfig(hcl.MergeFiles(l.files), primary, &varInputs{cli: cliVars, auto: autoVars})
	if err != nil {
		return nil, err
	}
	if !l.shape {
		// A module's port and whether it needs one are the module's to say.
		if err := cfg.validatePorts(); err != nil {
			return nil, err
		}
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
	return parse(src, filename, &loader{hooks: hooks})
}

// ParseShape is LoadShape for source bytes.
func ParseShape(src []byte, filename string) (*Config, error) {
	return parse(src, filename, &loader{shape: true})
}

func parse(src []byte, filename string, l *loader) (*Config, error) {
	l.parser = hclparse.NewParser()
	file, diags := l.parser.ParseHCL(src, filename)
	if diags.HasErrors() {
		return nil, diagError(l.parser, diags)
	}
	l.files = []*hcl.File{file}
	return l.buildConfig(file.Body, filename, nil)
}

// buildConfig validates a (possibly merged) HCL body into a Config.
func (l *loader) buildConfig(body hcl.Body, primaryPath string, inputs *varInputs) (*Config, error) {
	parser := l.parser
	if err := checkOldForms(parser, l.files); err != nil {
		return nil, err
	}
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "listen"}, {Name: "home"}, {Name: "data_dir"}, {Name: "stack"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "defaults"},
			{Type: "tls"},
			{Type: "variable", LabelNames: []string{"name"}},
			{Type: "locals"},
			{Type: "output", LabelNames: []string{"name"}},
			{Type: KindModule, LabelNames: []string{"type", "name"}},
			{Type: KindApp, LabelNames: []string{"name"}},
			{Type: KindService, LabelNames: []string{"name"}},
			{Type: KindTask, LabelNames: []string{"name"}},
		},
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
	var varBlocks, localsBlocks, outputBlocks, instanceBlocks, runnableBlocks []*hcl.Block
	seenDefaults, seenTLS := false, false
	var defaultsBlock, tlsBlock *hcl.Block
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
		case "variable":
			varBlocks = append(varBlocks, block)
		case "locals":
			localsBlocks = append(localsBlocks, block)
		case "output":
			outputBlocks = append(outputBlocks, block)
		case KindService, KindTask:
			runnableBlocks = append(runnableBlocks, block)
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
		case "stack":
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
	// Declare everything by name first, so a reference can be checked against
	// the whole stack whatever order the blocks are written in. Then the
	// reference graph and the module and app bodies in dependency order, then
	// the service and task bodies, then outputs (which may reference anything).
	declRanges := map[string]hcl.Range{}
	var pending []*pendingInstance
	for _, block := range instanceBlocks {
		ps, err := l.expandInstanceBlock(cfg, block, declRanges, ctx)
		if err != nil {
			return nil, err
		}
		pending = append(pending, ps...)
	}
	for _, block := range runnableBlocks {
		if err := l.declareRunnable(cfg, block, declRanges, ctx); err != nil {
			return nil, err
		}
	}
	known := cfg.declared()
	cfg.publishRunnables(ctx)
	if err := l.evaluate(cfg, pending, ctx, known); err != nil {
		return nil, err
	}
	if err := l.decodeRunnables(cfg, ctx, known); err != nil {
		return nil, err
	}
	if err := cfg.checkCycles(parser, declRanges); err != nil {
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

// reservedBlocks are the top-level block types that are neither a module, an
// app, a service nor a task.
var reservedBlocks = map[string]bool{
	"defaults": true, "tls": true,
	"variable": true, "locals": true, "output": true,
}

// futureBlocks are top-level block types a later release will read. Until
// then they are refused with a message saying so, and no module may take one
// as its type, so turning one on later breaks no stack.
var futureBlocks = map[string]bool{"recall": true}

// pluginBytes returns filename's source in the form a plugin decodes.
func (l *loader) pluginBytes(filename string) []byte {
	if src, ok := l.pluginSrc[filename]; ok {
		return src
	}
	f := l.parser.Files()[filename]
	if f == nil {
		return nil
	}
	if l.pluginSrc == nil {
		l.pluginSrc = map[string][]byte{}
	}
	src := pluginSource(f.Bytes, filename)
	l.pluginSrc[filename] = src
	return src
}
