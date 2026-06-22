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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agext/levenshtein"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/nerdmenot/doze/internal/engine"
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
	// Instances preserves declaration order from the file.
	Instances []*InstanceDecl

	path  string
	index map[string]*InstanceDecl
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

// Load reads and validates the doze configuration at path. path may be a single
// file (e.g. doze.hcl) — in which case a sibling doze.d/*.hcl directory is merged
// in if present — or a directory, in which case all of its *.hcl files are merged.
// Instance blocks may be split across files; root settings live in the main file.
func Load(path string) (*Config, error) {
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
	if err := checkBlockTypes(parser, hclFiles); err != nil {
		return nil, err
	}
	return buildConfig(parser, hcl.MergeFiles(hclFiles), primary)
}

// gatherConfigFiles resolves the ordered set of HCL files to merge, and the
// "primary" path used for diagnostics and the project slug. doze.hcl is always
// first so its root settings are authoritative; doze.d/*.hcl are appended sorted.
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
	files = []string{path}
	if dDir := filepath.Join(filepath.Dir(path), "doze.d"); dirExists(dDir) {
		extra, _ := filepath.Glob(filepath.Join(dDir, "*.hcl"))
		sort.Strings(extra)
		files = append(files, extra...)
	}
	return files, path, nil
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// Parse validates HCL source bytes. filename is used only for diagnostics.
// Engine drivers must already be registered (cmd/doze blank-imports them).
func Parse(src []byte, filename string) (*Config, error) {
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(src, filename)
	if diags.HasErrors() {
		return nil, diagError(parser, diags)
	}
	if err := checkBlockTypes(parser, []*hcl.File{file}); err != nil {
		return nil, err
	}
	return buildConfig(parser, file.Body, filename)
}

// buildConfig validates a (possibly merged) HCL body into a Config.
func buildConfig(parser *hclparse.Parser, body hcl.Body, primaryPath string) (*Config, error) {
	// The schema is built from the registered engines so each engine block type
	// is recognized; unknown block types become positioned diagnostics.
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "listen"}, {Name: "home"}, {Name: "data_dir"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "defaults"},
			{Type: "tls"},
		},
	}
	for _, t := range engine.Types() {
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
		if diags := gohcl.DecodeExpression(attr.Expr, nil, dst); diags.HasErrors() {
			return nil, diagError(parser, diags)
		}
	}

	if err := cfg.resolveHome(); err != nil {
		return nil, err
	}

	declRanges := map[string]hcl.Range{}
	seenDefaults, seenTLS := false, false
	for _, block := range content.Blocks {
		switch block.Type {
		case "defaults":
			if seenDefaults {
				return nil, posErr(parser, block.DefRange, "duplicate defaults block", "only one defaults block is allowed across all config files")
			}
			seenDefaults = true
			if err := cfg.decodeDefaults(parser, block); err != nil {
				return nil, err
			}
		case "tls":
			if seenTLS {
				return nil, posErr(parser, block.DefRange, "duplicate tls block", "only one tls block is allowed across all config files")
			}
			seenTLS = true
			if err := cfg.decodeTLS(parser, block); err != nil {
				return nil, err
			}
		default:
			if err := cfg.decodeInstance(parser, block, declRanges); err != nil {
				return nil, err
			}
		}
	}

	for _, inst := range cfg.Instances {
		cfg.index[inst.Name] = inst
	}
	return cfg, nil
}

// checkBlockTypes reports the first unknown top-level block type with a friendly,
// positioned diagnostic (with a "did you mean" suggestion) — better than HCL's
// generic "Unsupported block type". Operates on native-syntax bodies.
func checkBlockTypes(parser *hclparse.Parser, files []*hcl.File) error {
	known := map[string]bool{"defaults": true, "tls": true}
	var candidates []string
	for _, t := range engine.Types() {
		known[t] = true
		candidates = append(candidates, t)
	}
	for _, f := range files {
		body, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for _, blk := range body.Blocks {
			if known[blk.Type] {
				continue
			}
			detail := "not a known block type (expected defaults, tls, or an engine like " + strings.Join(candidates, ", ") + ")"
			if s := nearest(blk.Type, candidates); s != "" {
				detail = fmt.Sprintf("did you mean %q?", s)
			}
			rng := blk.TypeRange
			return diagError(parser, hcl.Diagnostics{{
				Severity: hcl.DiagError,
				Summary:  fmt.Sprintf("unknown block type %q", blk.Type),
				Detail:   detail,
				Subject:  &rng,
			}})
		}
	}
	return nil
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

func (cfg *Config) decodeDefaults(parser *hclparse.Parser, block *hcl.Block) error {
	var d hclDefaults
	if diags := gohcl.DecodeBody(block.Body, nil, &d); diags.HasErrors() {
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

func (cfg *Config) decodeTLS(parser *hclparse.Parser, block *hcl.Block) error {
	var t hclTLS
	if diags := gohcl.DecodeBody(block.Body, nil, &t); diags.HasErrors() {
		return diagError(parser, diags)
	}
	cfg.TLS = TLSSettings{Enabled: true, Cert: t.Cert, Key: t.Key, Required: t.Required}
	if (cfg.TLS.Cert == "") != (cfg.TLS.Key == "") {
		return posErr(parser, block.DefRange, "incomplete tls block", "set both cert and key, or neither (to auto-generate a self-signed cert)")
	}
	return nil
}

func (cfg *Config) decodeInstance(parser *hclparse.Parser, block *hcl.Block, declRanges map[string]hcl.Range) error {
	name := block.Labels[0]
	if first, dup := declRanges[name]; dup {
		return posErr(parser, block.DefRange,
			fmt.Sprintf("%s %q: instance %q is already declared", block.Type, name, name),
			"first declared at "+first.String())
	}
	declRanges[name] = block.DefRange

	var c common
	if diags := gohcl.DecodeBody(block.Body, nil, &c); diags.HasErrors() {
		return diagError(parser, diags)
	}
	drv, ok := engine.Lookup(block.Type)
	if !ok {
		return posErr(parser, block.DefRange, fmt.Sprintf("no driver registered for engine %q", block.Type), "")
	}
	if c.Version == "" {
		// Engines that ship inside doze (the local-AWS services) have no version.
		if _, versionless := drv.(engine.Versionless); !versionless {
			return posErr(parser, block.DefRange,
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
	}
	if dec, ok := drv.(engine.ConfigDecoder); ok {
		// Resolve relative paths (e.g. extension bundles) against the file that
		// declared this block, so split configs in doze.d/ behave intuitively.
		baseDir := "."
		if f := block.DefRange.Filename; f != "" {
			baseDir = filepath.Dir(f)
		} else if cfg.path != "" {
			baseDir = filepath.Dir(cfg.path)
		}
		spec, err := dec.DecodeConfig(c.Remain, nil, baseDir)
		if err != nil {
			return fmt.Errorf("%s %q: %w", block.Type, name, err)
		}
		inst.Spec = spec
	}
	cfg.index[name] = inst
	cfg.Instances = append(cfg.Instances, inst)
	return nil
}

// Lookup returns the declared instance by name, or nil.
func (c *Config) Lookup(name string) *InstanceDecl { return c.index[name] }

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

// diagError renders HCL diagnostics into a single Go error.
func diagError(parser *hclparse.Parser, diags hcl.Diagnostics) error {
	var buf bytes.Buffer
	wr := hcl.NewDiagnosticTextWriter(&buf, parser.Files(), 0, false)
	_ = wr.WriteDiagnostics(diags)
	return fmt.Errorf("invalid config:\n%s", buf.String())
}
