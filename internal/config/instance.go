// Instance block expansion (count/for_each stamping, engine-agnostic decode)
// and endpoint address assignment.
package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/doze-dev/doze-sdk/engine"

	"github.com/doze-dev/doze/internal/detect"
	"github.com/doze-dev/doze/internal/fault"
	"github.com/doze-dev/doze/internal/modules"
	"github.com/doze-dev/doze/internal/runspec"
)

// instanceMetaSchema is the meta-arguments of a module or app block, read
// before its body is decoded. (`depends_on` is an old form, refused with its
// new spelling.)
var instanceMetaSchema = &hcl.BodySchema{Attributes: []hcl.AttributeSchema{{Name: "count"}, {Name: "for_each"}, {Name: "depends_on"}, {Name: "enabled"}, {Name: "after"}}}

// expandInstanceBlock turns one module or app block into one or more pending
// instances. A plain block yields one; a `count`/`for_each` block is stamped into
// several, each with a flat derived name (label_key / label_index) and a child
// context exposing each.key/each.value or count.index. The meta-args are stripped
// before the driver decode so the engine's strict schema never sees them.
func (l *loader) expandInstanceBlock(cfg *Config, block *hcl.Block, declRanges map[string]hcl.Range, ctx *hcl.EvalContext) ([]*pendingInstance, error) {
	parser := l.parser
	head, err := headOf(parser, block)
	if err != nil {
		return nil, err
	}
	meta, restBody, diags := block.Body.PartialContent(instanceMetaSchema)
	if diags.HasErrors() {
		return nil, diagError(parser, diags)
	}
	countAttr, hasCount := meta.Attributes["count"]
	forEachAttr, hasForEach := meta.Attributes["for_each"]
	if hasCount && hasForEach {
		return nil, posErr(parser, block.DefRange, fmt.Sprintf("%s: set either count or for_each, not both", head), "")
	}
	enabled := true
	if en, ok := meta.Attributes["enabled"]; ok {
		v, ediags := en.Expr.Value(ctx)
		if ediags.HasErrors() {
			return nil, diagError(parser, ediags)
		}
		if v.IsNull() || v.Type() != cty.Bool {
			return nil, posErr(parser, en.Range, fmt.Sprintf("%s: enabled must be a boolean", head), "")
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
		p, err := l.buildPending(cfg, block, head, restBody, s.name(head.label), stampCtx, declRanges)
		if err != nil {
			return nil, err
		}
		p.decl.Enabled = enabled
		p.explicitDeps = explicit
		p.dependsOn = meta.Attributes["depends_on"]
		p.after = meta.Attributes["after"]
		out = append(out, p)
	}
	return out, nil
}

// buildPending decodes the engine-agnostic fields of one (possibly stamped)
// instance against stampCtx and registers it, deferring the driver body decode to
// the evaluation pass.
func (l *loader) buildPending(cfg *Config, block *hcl.Block, head blockHead, restBody hcl.Body, name string, stampCtx *hcl.EvalContext, declRanges map[string]hcl.Range) (*pendingInstance, error) {
	parser := l.parser
	what := title(head.kind, head.etype, name)
	if first, dup := declRanges[name]; dup {
		return nil, posErr(parser, block.DefRange,
			fmt.Sprintf("%s: the name %q is already taken", what, name),
			"first declared at "+first.String()+" — names are unique across a stack, whatever the kind")
	}
	declRanges[name] = block.DefRange

	// `wakes_on` is a later release's (waking a worker when work is queued for
	// it). Until that ships it is refused, not ignored: ignored, its references
	// would still make the named blocks dependencies, booted and held.
	if content, _, _ := restBody.PartialContent(&hcl.BodySchema{Attributes: []hcl.AttributeSchema{{Name: "wakes_on"}}}); content != nil {
		if attr, ok := content.Attributes["wakes_on"]; ok {
			return nil, posErr(parser, attr.Range,
				fmt.Sprintf("%s: `wakes_on` isn't in this release", what),
				"delete it; until it ships, a worker with no port stays on")
		}
	}

	// A module's `sleep` is doze's, not the module's: it is read here and taken
	// out of the body, so the module's strict decode never sees it. (An app
	// reads its own `sleep` through its engine.)
	var sleep runspec.Sleep
	var sleepRange hcl.Range
	var noRoutes bool
	if head.kind == KindModule {
		content, rest, diags := restBody.PartialContent(&hcl.BodySchema{Attributes: []hcl.AttributeSchema{{Name: "sleep"}, {Name: "routes"}}})
		if diags.HasErrors() {
			return nil, diagError(parser, diags)
		}
		if attr, ok := content.Attributes["sleep"]; ok {
			var raw string
			if diags := gohcl.DecodeExpression(attr.Expr, stampCtx, &raw); diags.HasErrors() {
				return nil, diagError(parser, diags)
			}
			s, err := runspec.ParseSleep(raw)
			if err != nil {
				return nil, posErr(parser, attr.Range, fmt.Sprintf("%s: %v", what, err), "")
			}
			sleep, sleepRange = s, attr.Range
		}
		// `routes = false` keeps the module's HTTP listeners on their ports only:
		// none of its names is served without a port (module-names.md).
		if attr, ok := content.Attributes["routes"]; ok {
			var on bool
			if diags := gohcl.DecodeExpression(attr.Expr, stampCtx, &on); diags.HasErrors() {
				return nil, posErr(parser, attr.Range, fmt.Sprintf("%s: `routes` is true or false", what),
					"routes = false serves the module's HTTP listeners on their ports only")
			}
			noRoutes = !on
		}
		restBody = rest
	}

	// A dotted version written as a number loses digits: 10.10 is the
	// number 10.1. Whole numbers are fine (17); anything with a dot must be
	// quoted.
	if content, _, _ := restBody.PartialContent(&hcl.BodySchema{Attributes: []hcl.AttributeSchema{{Name: "version"}}}); content != nil {
		if attr, ok := content.Attributes["version"]; ok {
			if lit, ok := attr.Expr.(*hclsyntax.LiteralValueExpr); ok && lit.Val.Type() == cty.Number {
				if bf := lit.Val.AsBigFloat(); !bf.IsInt() {
					src := strings.TrimSpace(string(attr.Expr.Range().SliceBytes(parser.Files()[attr.Range.Filename].Bytes)))
					return nil, posErr(parser, attr.Range,
						fmt.Sprintf("%s: version %s is a number, and a number drops trailing zeros (10.10 reads as 10.1)", what, src),
						fmt.Sprintf("write version = %q, quoted", src))
				}
			}
		}
	}

	var c common
	if diags := gohcl.DecodeBody(restBody, stampCtx, &c); diags.HasErrors() {
		return nil, diagError(parser, diags)
	}
	if head.kind == KindApp && (c.Version != "" || c.Listen != "") {
		arg := "version"
		if c.Version == "" {
			arg = "listen"
		}
		return nil, posErr(parser, block.DefRange, fmt.Sprintf("%s: `%s` is not an app argument", what, arg), "")
	}
	var drv engine.Driver
	if !l.shape || head.kind == KindApp {
		d, err := l.lookupDriver(block, head, c.Version)
		if err != nil {
			return nil, err
		}
		drv = d
	}
	if c.Version == "" {
		// Engines that ship inside doze (apps, the local-AWS services) have no
		// version. Without the driver the shape pass cannot tell, and leaves it
		// to the full load.
		_, versionless := drv.(engine.Versionless)
		if drv != nil && !versionless {
			return nil, posErr(parser, block.DefRange,
				fmt.Sprintf("%s: missing required \"version\"", what),
				"add e.g. version = 16 (a major) or version = \"16.14\" (exact)")
		}
		c.Version = "builtin"
	}
	if c.Port != 0 && (c.Port < 1 || c.Port > 65535) {
		return nil, posErr(parser, block.DefRange,
			fmt.Sprintf("%s: port %d is out of range", what, c.Port),
			"a port must be between 1 and 65535")
	}
	// A module that says no port takes the one its kind of server is known by,
	// so `module valkey "cache" { version = 9 }` is complete: an address is there
	// to reference. It is still never invented: a kind with no well-known port
	// (and an app, or a service) must say its own, and two that want the same one
	// are reported by validatePorts.
	//
	// The module knows its own port: once its driver has resolved, its
	// Describe's Port is asked first. doze's table (detect.ConventionalPort) is
	// only the fallback, for when no module can be asked: the offline shape
	// pass, `doze init` before anything is fetched, a module whose Describe
	// gives no port.
	if c.Port == 0 && c.Listen == "" && head.kind == KindModule && head.etype != "temporal" {
		if drv != nil {
			c.Port = l.hooks.defaultPort(head.etype)
		}
		if c.Port == 0 {
			c.Port = detect.ConventionalPort(head.etype)
		}
	}
	domains, cert, key, err := checkDomains(head, what, c.Domains, c.Cert, c.Key, blockBaseDir(cfg, block), l.shape)
	if err != nil {
		return nil, posErr(parser, block.DefRange, err.Error(), "")
	}
	inst := &InstanceDecl{
		Kind:    head.kind,
		Type:    head.etype,
		Name:    name,
		Version: engine.VersionSpec(c.Version),
		Listen:  c.Listen,
		Port:    c.Port,
		Domains: domains,
		Cert:    cert,
		Key:     key,
		Index:   len(cfg.Instances),
		Sleep:   sleep,

		NoRoutes: noRoutes,
	}
	cfg.index[name] = inst
	cfg.Instances = append(cfg.Instances, inst)
	return &pendingInstance{
		decl:     inst,
		drv:      drv,
		body:     restBody,
		remain:   c.Remain,
		ctx:      stampCtx,
		defRange: block.DefRange,
		// The count/for_each base; a plugin locates the source block by this, not
		// the expanded name.
		blockLabel: head.label,
		sleepRange: sleepRange,
		// Relative paths (extension bundles, lambda code dirs, an app's dir)
		// resolve against the file that declared the block. Absolute, because
		// module-decoded paths travel to plugin servers and the processes they
		// spawn, whose working directories are not the config dir.
		baseDir: blockBaseDir(cfg, block),
	}, nil
}

// lookupDriver resolves the engine behind a block. For a module this is where
// the module is fetched if it is not cached.
func (l *loader) lookupDriver(block *hcl.Block, head blockHead, version string) (engine.Driver, error) {
	parser := l.parser
	// Tell the module fetcher what engine version this block declares BEFORE the
	// lookup below may fetch the module, so selection selects a release that
	// supports it.
	l.hooks.requireEngine(head.etype, version)
	if drv, ok := engine.Lookup(head.etype); ok {
		return drv, nil
	}
	// The module fetcher may have a REAL failure recorded (signature, gate,
	// network) — surface that verbatim instead of "no module called".
	if err := l.hooks.lookupError(head.etype); err != nil {
		// A module whose host did not answer is not a mistake in the file:
		// say what could not be fetched and why, without the "config has a
		// mistake" framing or a snippet pointing at a correct block.
		if modules.IsUnreachable(err) {
			return nil, fault.With(fault.Invalid, &ModuleUnreachableError{Err: err})
		}
		return nil, posErr(parser, block.DefRange, err.Error(), "")
	}
	// Suggest from the registry catalog too: almost nothing is compiled in, so
	// the catalog is where the real candidates live.
	var candidates []string
	for _, t := range append(engine.Types(), l.hooks.engineNames()...) {
		if t != AppEngine {
			candidates = append(candidates, t)
		}
	}
	summary := fmt.Sprintf("no module called %q", head.etype)
	if s := nearest(head.etype, candidates); s != "" {
		summary += fmt.Sprintf(". Did you mean %q?", s)
	}
	return nil, posErr(parser, block.DefRange, summary, "")
}

// ModuleUnreachableError is a config that could not be loaded because a module
// it names could not be fetched: its host did not answer. The file may well be
// right; nothing could check it. Its message says so, and what to do next.
type ModuleUnreachableError struct{ Err error }

func (e *ModuleUnreachableError) Error() string {
	return e.Err.Error() + "\n  this is the network or the registry, not doze.hcl · doze doctor checks the registry"
}

func (e *ModuleUnreachableError) Unwrap() error { return e.Err }

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
	return "", fmt.Errorf("%s has no port — add `port = NNNN` to the block "+
		"(e.g. port = 5432); doze does not auto-assign ports", decl.Title())
}
