// Package config parses doze.hcl into a validated, engine-agnostic view.
//
// The root (listen, home, data_dir, defaults, tls) is fixed; each database
// engine contributes its own block type (postgres, valkey, …). For every block
// whose keyword matches a registered engine driver, config reads the common
// fields (version, listen) and hands the rest of the block body to the driver's
// ConfigDecoder, so config itself knows nothing engine-specific.
package config

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/agext/levenshtein"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/doze-dev/doze-sdk/engine"
)

// Defaults for fields the user did not specify.
const (
	DefaultListen      = "127.0.0.1:6432"
	DefaultIdleTimeout = 5 * time.Minute
)

// Config is the validated, typed view of a doze.hcl file.
type Config struct {
	// Listen is the default client-facing address; instances may override it.
	Listen string
	// Home is the global doze home (shared toolchains + cache), deduped across
	// projects. Resolved from `home`, then $DOZE_HOME, then ~/.doze.
	Home string
	// DataDir is this project's state directory; defaults to
	// <Home>/projects/<slug> so projects never collide.
	DataDir string
	// Defaults is the generic tuning profile (engine-agnostic).
	Defaults Defaults
	// TLS configures client-facing TLS termination.
	TLS TLSSettings
	// Modules configures the out-of-process engine plugin fetcher (source + pins).
	Modules ModulesConfig
	// Instances preserves declaration order from the file.
	Instances []*InstanceDecl
	// Outputs are the declared output values, keyed by name (declaration order in
	// OutputOrder), resolved against the final evaluation context.
	Outputs     map[string]Output
	OutputOrder []string

	path  string
	index map[string]*InstanceDecl
}

// Output is a declared output value: the connection strings or facts a stack
// exposes. Surfaced by `doze output`.
type Output struct {
	Name        string
	Value       string // rendered value
	Description string
	Sensitive   bool
}

// Defaults holds engine-agnostic tuning. Engine-specific tuning (Postgres
// shared_buffers, fsync, …) lives inside that engine's config block.
type Defaults struct {
	IdleTimeout time.Duration
}

// TLSSettings configures TLS termination between clients and the proxy.
type TLSSettings struct {
	Enabled  bool
	Cert     string
	Key      string
	Required bool
}

// InstanceDecl is one declared instance: a database server of some engine.
type InstanceDecl struct {
	Type    string              // engine type / block keyword ("postgres")
	Name    string              // block label
	Version engine.VersionSpec  // "16" (major) or "16.14" (exact)
	Listen  string              // optional per-instance endpoint override
	Spec    engine.EngineConfig // engine-specific config (decoded by the driver)
	// Deps are the other declared instances this one must boot first (e.g. an sns
	// instance referencing sqs.jobs), each with a readiness condition. Derived
	// from the config reference graph (every reference is a Healthy dependency)
	// and any explicit `depends_on`; the runtime boots and holds them first.
	Index int                 // declaration order, used for endpoint address assignment
	Deps  []engine.Dependency // dependencies, in reference order
	// Enabled defaults to true; `enabled = false` declares the instance but leaves it
	// paused — not booted by up/wake, not converged or pruned by sync (its data is
	// preserved), shown as "disabled" in the tree. Re-enabling brings it back as-is.
	Enabled bool
}

// common is the partial-decode target for the fields config reads from every
// engine block; the rest of the body goes to the driver.
type common struct {
	Version string   `hcl:"version,optional"`
	Listen  string   `hcl:"listen,optional"`
	Remain  hcl.Body `hcl:",remain"`
}

type hclTLS struct {
	Cert     string `hcl:"cert,optional"`
	Key      string `hcl:"key,optional"`
	Required bool   `hcl:"required,optional"`
}

type hclDefaults struct {
	IdleTimeout string `hcl:"idle_timeout,optional"`
}

type hclModules struct {
	Mirror  string      `hcl:"mirror,optional"`
	Enabled bool        `hcl:"enabled,optional"`
	Modules []hclModule `hcl:"module,block"`
}

type hclModule struct {
	Name    string `hcl:"name,label"`
	Version string `hcl:"version,optional"`
	Source  string `hcl:"source,optional"`
}

// Load reads and validates the doze configuration at path, with no variable
// overrides. See LoadWithVars.
func Load(path string) (*Config, error) { return LoadWithVars(path, nil) }

// LoadWithVars reads and validates the doze configuration at path. path may be a
// single file (e.g. doze.hcl) — in which case every sibling *.doze.hcl is merged
// in — or a directory, in which case all of its *.hcl files are merged. cliVars
// are --var overrides (highest precedence). Variable values also come from
// DOZE_VAR_<name> env vars and sibling *.auto.doze.vars files.
func LoadWithVars(path string, cliVars map[string]string) (*Config, error) {
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
	return buildConfig(parser, hcl.MergeFiles(hclFiles), primary, &varInputs{cli: cliVars, auto: autoVars}, engineBlockTypes(hclFiles))
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
func Parse(src []byte, filename string) (*Config, error) {
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(src, filename)
	if diags.HasErrors() {
		return nil, diagError(parser, diags)
	}
	return buildConfig(parser, file.Body, filename, nil, engineBlockTypes([]*hcl.File{file}))
}

// buildConfig validates a (possibly merged) HCL body into a Config. engineTypes is
// the set of engine block types declared in the source (every non-reserved labeled
// block); each is accepted into the schema and validated to a real driver later.
func buildConfig(parser *hclparse.Parser, body hcl.Body, primaryPath string, inputs *varInputs, engineTypes []string) (*Config, error) {
	// Accept every declared engine block type — config can't enumerate engines
	// (they're out-of-process modules), so it trusts the type and resolves it later.
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "listen"}, {Name: "home"}, {Name: "data_dir"},
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
	if modulesConfigurer != nil {
		modulesConfigurer(cfg.Modules)
	}

	// Instance shells (engine-agnostic fields), then the reference graph + driver
	// bodies in dependency order, then outputs (which may reference everything).
	declRanges := map[string]hcl.Range{}
	var pending []*pendingInstance
	for _, block := range instanceBlocks {
		ps, err := cfg.expandInstanceBlock(parser, block, declRanges, ctx)
		if err != nil {
			return nil, err
		}
		pending = append(pending, ps...)
	}
	if err := cfg.evaluate(parser, pending, ctx); err != nil {
		return nil, err
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

// nearest returns the closest candidate within a small edit distance, or "".
func nearest(s string, candidates []string) string {
	best, bestD := "", 1<<30
	for _, c := range candidates {
		if d := levenshtein.Distance(s, c, nil); d < bestD {
			best, bestD = c, d
		}
	}
	if bestD <= 3 {
		return best
	}
	return ""
}

func (cfg *Config) resolveHome() error {
	if cfg.Home == "" {
		cfg.Home = os.Getenv(EnvHome)
	}
	if cfg.Home == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolving home dir for default doze home: %w", err)
		}
		cfg.Home = filepath.Join(home, ".doze")
	}
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(cfg.Home, "projects", projectSlug(cfg.path))
	}
	return nil
}

func (cfg *Config) decodeDefaults(parser *hclparse.Parser, block *hcl.Block, ctx *hcl.EvalContext) error {
	var d hclDefaults
	if diags := gohcl.DecodeBody(block.Body, ctx, &d); diags.HasErrors() {
		return diagError(parser, diags)
	}
	if d.IdleTimeout != "" {
		td, err := time.ParseDuration(d.IdleTimeout)
		if err != nil {
			return posErr(parser, block.DefRange, "invalid idle_timeout", fmt.Sprintf("%q is not a valid duration (try \"5m\", \"30s\")", d.IdleTimeout))
		}
		if td < 0 {
			return posErr(parser, block.DefRange, "invalid idle_timeout", "must not be negative")
		}
		cfg.Defaults.IdleTimeout = td
	}
	return nil
}

func (cfg *Config) decodeTLS(parser *hclparse.Parser, block *hcl.Block, ctx *hcl.EvalContext) error {
	var t hclTLS
	if diags := gohcl.DecodeBody(block.Body, ctx, &t); diags.HasErrors() {
		return diagError(parser, diags)
	}
	cfg.TLS = TLSSettings{Enabled: true, Cert: t.Cert, Key: t.Key, Required: t.Required}
	if (cfg.TLS.Cert == "") != (cfg.TLS.Key == "") {
		return posErr(parser, block.DefRange, "incomplete tls block", "set both cert and key, or neither (to auto-generate a self-signed cert)")
	}
	return nil
}

// ModulesConfig is the decoded `modules {}` block: where to fetch out-of-process
// engine plugins from and which versions to pin. It is applied to the plugin
// resolver before instance decode (see modulesConfigurer).
type ModulesConfig struct {
	Mirror   string            // registry base (overrides DOZE_MODULES_MIRROR)
	Enabled  bool              // fetch plugin modules (true also when a mirror is set)
	Versions map[string]string // engine type -> pinned module version ("" = default channel)
	Sources  map[string]string // engine type -> source address override ("" = doze/<type>)
}

// modulesConfigurer, when registered (by cmd/doze), is handed the decoded
// modules{} block so it can point the plugin module fetcher at the configured
// mirror/versions before any instance's driver is resolved. It lives as a package
// hook to keep internal/config from importing the module fetcher.
var modulesConfigurer func(ModulesConfig)

// SetModulesConfigurer registers the callback invoked with the modules{} block
// during config load (before instance decode).
func SetModulesConfigurer(fn func(ModulesConfig)) { modulesConfigurer = fn }

func (cfg *Config) decodeModules(parser *hclparse.Parser, block *hcl.Block, ctx *hcl.EvalContext) error {
	var m hclModules
	if diags := gohcl.DecodeBody(block.Body, ctx, &m); diags.HasErrors() {
		return diagError(parser, diags)
	}
	mc := ModulesConfig{
		Mirror:   m.Mirror,
		Enabled:  m.Enabled || m.Mirror != "",
		Versions: map[string]string{},
		Sources:  map[string]string{},
	}
	for _, md := range m.Modules {
		mc.Versions[md.Name] = md.Version
		mc.Sources[md.Name] = md.Source
	}
	cfg.Modules = mc
	return nil
}

// expandInstanceBlock turns one instance block into one or more pending
// instances. A plain block yields one; a `count`/`for_each` block is stamped into
// several, each with a flat derived name (label_key / label_index) and a child
// context exposing each.key/each.value or count.index. The meta-args are stripped
// before the driver decode so the engine's strict schema never sees them.
func (cfg *Config) expandInstanceBlock(parser *hclparse.Parser, block *hcl.Block, declRanges map[string]hcl.Range, ctx *hcl.EvalContext) ([]*pendingInstance, error) {
	metaSchema := &hcl.BodySchema{Attributes: []hcl.AttributeSchema{{Name: "count"}, {Name: "for_each"}, {Name: "depends_on"}, {Name: "enabled"}}}
	meta, restBody, diags := block.Body.PartialContent(metaSchema)
	if diags.HasErrors() {
		return nil, diagError(parser, diags)
	}
	countAttr, hasCount := meta.Attributes["count"]
	forEachAttr, hasForEach := meta.Attributes["for_each"]
	if hasCount && hasForEach {
		return nil, posErr(parser, block.DefRange, fmt.Sprintf("%s %q: set either count or for_each, not both", block.Type, block.Labels[0]), "")
	}
	enabled := true
	if en, ok := meta.Attributes["enabled"]; ok {
		v, ediags := en.Expr.Value(ctx)
		if ediags.HasErrors() {
			return nil, diagError(parser, ediags)
		}
		if v.IsNull() || v.Type() != cty.Bool {
			return nil, posErr(parser, en.Range, fmt.Sprintf("%s %q: enabled must be a boolean", block.Type, block.Labels[0]), "")
		}
		enabled = v.True()
	}
	var explicit map[string]engine.Condition
	if dep, ok := meta.Attributes["depends_on"]; ok {
		e, perr := parseDependsOn(parser, dep, ctx)
		if perr != nil {
			return nil, perr
		}
		explicit = e
	}

	stamps, err := cfg.instanceStamps(parser, block, countAttr, forEachAttr, hasCount, hasForEach, ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*pendingInstance, 0, len(stamps))
	for _, s := range stamps {
		stampCtx := ctx
		if s.vars != nil {
			stampCtx = ctx.NewChild()
			stampCtx.Variables = s.vars
		}
		p, err := cfg.buildPending(parser, block, restBody, s.name(block.Labels[0]), stampCtx, declRanges)
		if err != nil {
			return nil, err
		}
		p.decl.Enabled = enabled
		p.explicitDeps = explicit
		out = append(out, p)
	}
	return out, nil
}

// buildPending decodes the engine-agnostic fields of one (possibly stamped)
// instance against stampCtx and registers it, deferring the driver body decode to
// the evaluation pass.
func (cfg *Config) buildPending(parser *hclparse.Parser, block *hcl.Block, restBody hcl.Body, name string, stampCtx *hcl.EvalContext, declRanges map[string]hcl.Range) (*pendingInstance, error) {
	if first, dup := declRanges[name]; dup {
		return nil, posErr(parser, block.DefRange,
			fmt.Sprintf("%s %q: instance %q is already declared", block.Type, name, name),
			"first declared at "+first.String())
	}
	declRanges[name] = block.DefRange

	var c common
	if diags := gohcl.DecodeBody(restBody, stampCtx, &c); diags.HasErrors() {
		return nil, diagError(parser, diags)
	}
	drv, ok := engine.Lookup(block.Type)
	if !ok {
		// The type passed the schema (any labeled block is a candidate engine) but
		// resolves to no driver — not built in, and no module provides it.
		detail := "no engine of this type is built in or provided by a module"
		if s := nearest(block.Type, engine.Types()); s != "" {
			detail = fmt.Sprintf("did you mean %q?", s)
		}
		return nil, posErr(parser, block.DefRange, fmt.Sprintf("unknown engine %q", block.Type), detail)
	}
	if c.Version == "" {
		// Engines that ship inside doze (the local-AWS services) have no version.
		if _, versionless := drv.(engine.Versionless); !versionless {
			return nil, posErr(parser, block.DefRange,
				fmt.Sprintf("%s %q: missing required \"version\"", block.Type, name),
				"add e.g. version = 16 (a major) or version = \"16.14\" (exact)")
		}
		c.Version = "builtin"
	}
	inst := &InstanceDecl{
		Type:    block.Type,
		Name:    name,
		Version: engine.VersionSpec(c.Version),
		Listen:  c.Listen,
		Index:   len(cfg.Instances),
	}
	// Resolve relative paths (e.g. extension bundles) against the file that
	// declared this block, so split configs behave intuitively.
	baseDir := "."
	if f := block.DefRange.Filename; f != "" {
		baseDir = filepath.Dir(f)
	} else if cfg.path != "" {
		baseDir = filepath.Dir(cfg.path)
	}
	cfg.index[name] = inst
	cfg.Instances = append(cfg.Instances, inst)
	return &pendingInstance{
		decl:       inst,
		drv:        drv,
		body:       restBody,
		remain:     c.Remain,
		ctx:        stampCtx,
		defRange:   block.DefRange,
		blockLabel: block.Labels[0], // the count/for_each base; the plugin locates the source block by this, not the expanded name
		baseDir:    baseDir,
	}, nil
}

// Lookup returns the declared instance by name, or nil.
func (c *Config) Lookup(name string) *InstanceDecl { return c.index[name] }

// InstanceAddr returns the client-facing address assigned to a declared instance:
// a per-instance `listen` override wins; otherwise, for a TCP base each instance
// gets base_port+Index, and for a unix base a per-instance socket beside it. It is
// the single source of truth for endpoint assignment (endpoints and the reference
// evaluator both call it).
func (c *Config) InstanceAddr(decl *InstanceDecl) (string, error) {
	if decl.Listen != "" {
		return decl.Listen, nil
	}
	// A supervised process binds its own port; advertise that address instead of a
	// doze proxy slot (the daemon opens no listener for it). The proxy-port index it
	// would otherwise occupy is simply left unbound.
	if drv, ok := engine.Lookup(decl.Type); ok {
		if pb, ok := drv.(engine.PortBinder); ok {
			if addr, ok := pb.AdvertisedAddr(engine.Instance{Name: decl.Name, Type: decl.Type, Spec: decl.Spec}); ok {
				return addr, nil
			}
		}
	}
	if path, ok := strings.CutPrefix(c.Listen, "unix:"); ok {
		return "unix:" + filepath.Join(filepath.Dir(path), decl.Name+".sock"), nil
	}
	host, portStr, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return "", fmt.Errorf("invalid listen address %q: %w", c.Listen, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", fmt.Errorf("invalid listen port %q: %w", portStr, err)
	}
	return net.JoinHostPort(host, strconv.Itoa(port+decl.Index)), nil
}

// Add registers an additional instance at runtime. It is used to inject
// synthetic instances (e.g. `doze ephemeral`) that are not in the file.
func (c *Config) Add(decl *InstanceDecl) {
	if c.index == nil {
		c.index = map[string]*InstanceDecl{}
	}
	c.Instances = append(c.Instances, decl)
	c.index[decl.Name] = decl
}

// Path is the file this config was loaded from (empty for in-memory parses).
func (c *Config) Path() string { return c.path }

// posErr builds a single positioned diagnostic (file/line/snippet) so validation
// errors point at the offending block, like HCL's own syntax errors.
func posErr(parser *hclparse.Parser, rng hcl.Range, summary, detail string) error {
	r := rng
	return diagError(parser, hcl.Diagnostics{{
		Severity: hcl.DiagError,
		Summary:  summary,
		Detail:   detail,
		Subject:  &r,
	}})
}

// diagError renders HCL diagnostics into a single Go error, after augmenting any
// with a doze-specific fix hint.
func diagError(parser *hclparse.Parser, diags hcl.Diagnostics) error {
	addFixHints(diags)
	var buf bytes.Buffer
	wr := hcl.NewDiagnosticTextWriter(&buf, parser.Files(), 0, false)
	_ = wr.WriteDiagnostics(diags)
	return fmt.Errorf("invalid config:\n%s", buf.String())
}

// addFixHints appends an actionable fix to HCL grammar errors that are easy to hit
// but cryptic on their own. The common one for the short health/restart blocks is
// writing several arguments on one line, which HCL's single-line block grammar
// forbids — the raw message never says "use multiple lines", so we add that.
func addFixHints(diags hcl.Diagnostics) {
	const hint = "\n\nHCL single-line blocks take exactly one argument; put each on its " +
		"own line instead:\n" +
		"    health {\n" +
		"      http     = \"http://localhost:8080/health\"\n" +
		"      interval = \"2s\"\n" +
		"    }"
	for _, d := range diags {
		if d == nil {
			continue
		}
		if strings.Contains(d.Summary, "single-argument block") && !strings.Contains(d.Detail, "own line") {
			d.Detail += hint
		}
	}
}
