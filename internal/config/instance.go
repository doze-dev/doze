// Instance block expansion (count/for_each stamping, engine-agnostic decode)
// and endpoint address assignment.
package config

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/zclconf/go-cty/cty"

	"github.com/doze-dev/doze-sdk/engine"
)

// expandInstanceBlock turns one instance block into one or more pending
// instances. A plain block yields one; a `count`/`for_each` block is stamped into
// several, each with a flat derived name (label_key / label_index) and a child
// context exposing each.key/each.value or count.index. The meta-args are stripped
// before the driver decode so the engine's strict schema never sees them.
func (l *loader) expandInstanceBlock(cfg *Config, block *hcl.Block, declRanges map[string]hcl.Range, ctx *hcl.EvalContext) ([]*pendingInstance, error) {
	parser := l.parser
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
		p, err := l.buildPending(cfg, block, restBody, s.name(block.Labels[0]), stampCtx, declRanges)
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
func (l *loader) buildPending(cfg *Config, block *hcl.Block, restBody hcl.Body, name string, stampCtx *hcl.EvalContext, declRanges map[string]hcl.Range) (*pendingInstance, error) {
	parser := l.parser
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
	// Tell the module fetcher what engine version this block declares BEFORE the
	// lookup below may fetch the module, so selection selects a release that
	// supports it.
	l.hooks.requireEngine(block.Type, c.Version)
	drv, ok := engine.Lookup(block.Type)
	if !ok {
		// The module fetcher may have a REAL failure recorded (signature, gate,
		// network) — surface that verbatim instead of "unknown engine".
		if err := l.hooks.lookupError(block.Type); err != nil {
			return nil, posErr(parser, block.DefRange, err.Error(), "")
		}
		// The type passed the schema (any labeled block is a candidate engine) but
		// resolves to no driver — not built in, and no module provides it. Suggest
		// from the registry catalog too: at this point almost nothing is compiled
		// in, so the catalog is where the real candidates live.
		candidates := append(engine.Types(), l.hooks.engineNames()...)
		detail := "no engine of this type is built in or provided by a module"
		if s := nearest(block.Type, candidates); s != "" {
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
	if c.Port != 0 && (c.Port < 1 || c.Port > 65535) {
		return nil, posErr(parser, block.DefRange,
			fmt.Sprintf("%s %q: port %d is out of range", block.Type, name, c.Port),
			"a port must be between 1 and 65535")
	}
	inst := &InstanceDecl{
		Type:    block.Type,
		Name:    name,
		Version: engine.VersionSpec(c.Version),
		Listen:  c.Listen,
		Port:    c.Port,
		Index:   len(cfg.Instances),
	}
	// Resolve relative paths (e.g. extension bundles, lambda code dirs)
	// against the file that declared this block, so split configs behave
	// intuitively. The result must be ABSOLUTE: module-decoded paths travel to
	// plugin servers and their spawned processes, whose working directories
	// are not the config dir — a relative baseDir silently breaks them.
	baseDir := "."
	if f := block.DefRange.Filename; f != "" {
		baseDir = filepath.Dir(f)
	} else if cfg.path != "" {
		baseDir = filepath.Dir(cfg.path)
	}
	if !filepath.IsAbs(baseDir) {
		if abs, err := filepath.Abs(baseDir); err == nil {
			baseDir = abs
		}
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

// InstanceAddr returns the client-facing address assigned to a declared instance:
// a per-instance `listen` override wins; otherwise, for a TCP base each instance
// gets base_port+Index, and for a unix base a per-instance socket beside it. It is
// the single source of truth for endpoint assignment (endpoints and the reference
// evaluator both call it).
func (c *Config) InstanceAddr(decl *InstanceDecl) (string, error) {
	// A full `listen = "host:port"` override wins (e.g. binding 0.0.0.0 or a socket).
	if decl.Listen != "" {
		return decl.Listen, nil
	}
	// Otherwise the instance must declare its client-facing port. doze does not
	// auto-assign ports — be explicit, like you would for any real service.
	if decl.Port != 0 {
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(decl.Port)), nil
	}
	// A supervised process may be portless — a background worker with no endpoint
	// binds nothing and doze fronts nothing, so it needs no port. Any other engine
	// must declare one: doze opens a proxy listener for it and won't guess a port.
	// (Supervised is the daemon's own "no proxy" signal — unlike PortBinder, the
	// module adapter doesn't satisfy it unless the plugin actually advertises it.)
	if drv, ok := engine.Lookup(decl.Type); ok {
		if lc, ok := drv.(engine.Lifecycle); ok && lc.Supervised(engine.Instance{Name: decl.Name, Type: decl.Type, Spec: decl.Spec}) {
			return "", nil
		}
	}
	return "", fmt.Errorf("%s %q has no port — add `port = NNNN` to the block "+
		"(e.g. port = 5432); doze does not auto-assign ports", decl.Type, decl.Name)
}
