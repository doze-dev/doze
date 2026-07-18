package config

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/doze-dev/doze-sdk/engine"
)

// pendingInstance carries an instance whose engine-agnostic fields are decoded
// but whose driver body is decoded later, in dependency order, by evaluate.
type pendingInstance struct {
	decl         *InstanceDecl
	drv          engine.Driver
	body         hcl.Body                    // block body (meta-args stripped), for reference extraction
	remain       hcl.Body                    // body minus common fields, for the driver decode
	ctx          *hcl.EvalContext            // the context this stamp decodes against (carries each/count)
	explicitDeps map[string]engine.Condition // explicit depends_on: instance name -> condition
	defRange     hcl.Range
	blockLabel   string // the source block's label (the count/for_each base, e.g. "assets")
	baseDir      string
}

// evaluate is config's second pass: it derives the cross-instance reference graph
// (each instance's references to other instances build its dependency edges),
// topologically orders the instances, then decodes each driver body against an
// evaluation context that already holds the attributes of every instance it
// references — so `sqs = sqs.jobs.name` resolves to a value and the dependency is
// recorded without any hand-declared DependsOn.
// fileBytes returns the raw source of a parsed file (for a plugin's remote decode).
func fileBytes(parser *hclparse.Parser, filename string) []byte {
	if f := parser.Files()[filename]; f != nil {
		return f.Bytes
	}
	return nil
}

// mergedVars flattens the global eval context with a stamp's (each/count) variables
// so a plugin receives every variable a reference in its block could resolve.
func mergedVars(global, stamp *hcl.EvalContext) map[string]cty.Value {
	out := make(map[string]cty.Value, len(global.Variables))
	for k, v := range global.Variables {
		out[k] = v
	}
	if stamp != nil && stamp != global {
		for k, v := range stamp.Variables {
			out[k] = v
		}
	}
	return out
}

func (l *loader) evaluate(cfg *Config, pending []*pendingInstance, ctx *hcl.EvalContext) error {
	parser := l.parser
	// An engine type is "known" (a resource reference root, not var/local) if some
	// declared instance uses it — config can't enumerate engines (they're modules),
	// so the declared set is the source of truth for reference resolution.
	knownTypes := map[string]bool{}
	byName := map[string]*pendingInstance{}
	for _, p := range pending {
		knownTypes[p.decl.Type] = true
		byName[p.decl.Name] = p
	}

	// 1. Reference extraction: each instance's dependency names, validated.
	for _, p := range pending {
		deps, err := cfg.instanceDeps(parser, p, knownTypes)
		if err != nil {
			return err
		}
		p.decl.Deps = deps
	}

	// 2. Topological order (dependencies before dependents); detect cycles.
	order, err := topoOrder(parser, pending, byName)
	if err != nil {
		return err
	}

	// 2b. An enabled instance must not depend on a disabled (paused) one — it could
	// never boot. Fail loud so `enabled = false` can't silently break the stack.
	for _, p := range pending {
		if !p.decl.Enabled {
			continue
		}
		for _, d := range p.decl.Deps {
			if dep, ok := byName[d.Name]; ok && !dep.decl.Enabled {
				return posErr(parser, p.defRange,
					fmt.Sprintf("%s %q depends on %q, which is disabled", p.decl.Type, p.decl.Name, d.Name),
					fmt.Sprintf("enable %q, or disable %q as well", d.Name, p.decl.Name))
			}
		}
	}

	// 3. Decode each driver body in order, growing the evaluation context (which
	// already holds var.*/local.*) with each instance's attribute object under
	// <type>.<name>.
	byType := map[string]map[string]cty.Value{}
	for _, p := range order {
		// p.ctx is the stamp-specific context (with each/count) for a for_each/count
		// instance; it chains to the shared ctx so var/local/resources resolve too.
		decodeCtx := p.ctx
		if decodeCtx == nil {
			decodeCtx = ctx
		}
		switch dec := p.drv.(type) {
		case engine.ConfigDecoder: // in-tree engine
			spec, err := dec.DecodeConfig(p.remain, decodeCtx, p.baseDir, p.decl.Version)
			if err != nil {
				return fmt.Errorf("%s %q: %w", p.decl.Type, p.decl.Name, err)
			}
			p.decl.Spec = spec
		case engine.RemoteDecoder: // out-of-process plugin: it decodes its own block
			src := fileBytes(parser, p.defRange.Filename)
			if src == nil {
				return fmt.Errorf("%s %q: cannot locate source for remote decode", p.decl.Type, p.decl.Name)
			}
			// Find the block by its source label (the count/for_each base), not the
			// expanded instance name; the each/count vars in decodeCtx differentiate stamps.
			spec, err := dec.DecodeRemote(src, p.defRange.Filename, p.decl.Type, p.blockLabel, mergedVars(ctx, decodeCtx), p.baseDir, p.decl.Version)
			if err != nil {
				// The plugin re-parses the source under a synthesized name
				// ("<label>.doze.hcl"); its positions are relative to the real file,
				// so put the real filename back before the user sees it.
				msg := strings.ReplaceAll(err.Error(), p.blockLabel+".doze.hcl", p.defRange.Filename)
				// The block was decoded by a specific module release; say which, and
				// (best-effort, error path only) whether an upgrade would help — a
				// schema mismatch on an old plugin should read as its fix, not as an
				// opaque "unsupported argument".
				return fmt.Errorf("%s %q: %s%s", p.decl.Type, p.decl.Name, msg, l.hooks.remoteDecodeErrSuffix(p.decl.Type))
			}
			p.decl.Spec = spec
		}
		attrs, err := cfg.attributesFor(p)
		if err != nil {
			return err
		}
		if byType[p.decl.Type] == nil {
			byType[p.decl.Type] = map[string]cty.Value{}
		}
		byType[p.decl.Type][p.decl.Name] = attrs
		ctx.Variables[p.decl.Type] = cty.ObjectVal(byType[p.decl.Type])
	}
	return nil
}

// instanceDeps returns the other declared instances p must boot first: every
// instance it references (a Healthy dependency) plus any explicit depends_on
// (which may set a stronger or weaker condition). A reference whose root is an
// engine type and whose next segment names a declared instance (e.g. sqs.jobs) is
// a dependency; an unknown instance is a positioned error. Self references drop.
func (cfg *Config) instanceDeps(parser *hclparse.Parser, p *pendingInstance, knownTypes map[string]bool) ([]engine.Dependency, error) {
	cond := map[string]engine.Condition{}
	var order []string
	// Reference-derived dependencies (always Healthy).
	for _, t := range referencedTraversals(p.body) {
		root := t.RootName()
		if !knownTypes[root] {
			continue // var./local./functions are handled elsewhere; not a resource ref
		}
		name, ok := traversalName(t)
		if !ok || name == p.decl.Name {
			continue
		}
		if _, declared := cfg.index[name]; !declared {
			rng := t.SourceRange()
			return nil, posErr(parser, rng,
				fmt.Sprintf("reference to undeclared instance %q", root+"."+name),
				fmt.Sprintf("no %s instance named %q is declared", root, name))
		}
		if _, ok := cond[name]; !ok {
			order = append(order, name)
			cond[name] = engine.Healthy
		}
	}
	// Explicit depends_on conditions (override the default, add new edges).
	for _, name := range sortedConditionKeys(p.explicitDeps) {
		if name == p.decl.Name {
			continue
		}
		if _, declared := cfg.index[name]; !declared {
			return nil, posErr(parser, p.defRange,
				fmt.Sprintf("%s %q: depends_on references undeclared instance %q", p.decl.Type, p.decl.Name, name), "")
		}
		if _, ok := cond[name]; !ok {
			order = append(order, name)
		}
		cond[name] = p.explicitDeps[name]
	}
	deps := make([]engine.Dependency, 0, len(order))
	for _, n := range order {
		deps = append(deps, engine.Dependency{Name: n, Condition: cond[n]})
	}
	return deps, nil
}

// sortedConditionKeys returns m's keys sorted, for deterministic dep order.
func sortedConditionKeys(m map[string]engine.Condition) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// referencedTraversals collects every variable traversal in body, recursing into
// nested blocks. Native HCL only (doze parses native syntax); other bodies yield
// none.
func referencedTraversals(body hcl.Body) []hcl.Traversal {
	hb, ok := body.(*hclsyntax.Body)
	if !ok {
		return nil
	}
	var out []hcl.Traversal
	var walk func(b *hclsyntax.Body)
	walk = func(b *hclsyntax.Body) {
		for _, attr := range b.Attributes {
			out = append(out, attr.Expr.Variables()...)
		}
		for _, blk := range b.Blocks {
			walk(blk.Body)
		}
	}
	walk(hb)
	return out
}

// traversalName returns the instance-name segment of a resource traversal
// (the attribute after the type root, e.g. "jobs" in sqs.jobs.name).
func traversalName(t hcl.Traversal) (string, bool) {
	if len(t) < 2 {
		return "", false
	}
	if a, ok := t[1].(hcl.TraverseAttr); ok {
		return a.Name, true
	}
	return "", false
}

// topoOrder returns pending instances in dependency order (each instance after
// the instances it references). A cycle is a positioned error.
func topoOrder(parser *hclparse.Parser, pending []*pendingInstance, byName map[string]*pendingInstance) ([]*pendingInstance, error) {
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS stack
		black = 2 // done
	)
	state := map[string]int{}
	var order []*pendingInstance
	var visit func(p *pendingInstance, stack []string) error
	visit = func(p *pendingInstance, stack []string) error {
		switch state[p.decl.Name] {
		case black:
			return nil
		case gray:
			cycle := append(append([]string{}, stack...), p.decl.Name)
			return posErr(parser, p.defRange, "dependency cycle between instances",
				strings.Join(cycle, " → "))
		}
		state[p.decl.Name] = gray
		for _, dn := range p.decl.Deps {
			if dep := byName[dn.Name]; dep != nil {
				if err := visit(dep, append(stack, p.decl.Name)); err != nil {
					return err
				}
			}
		}
		state[p.decl.Name] = black
		order = append(order, p)
		return nil
	}
	for _, p := range pending {
		if err := visit(p, nil); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// attributesFor builds the cty object exposed under <type>.<name> for references:
// the generic baseline (name, engine, address, host/port or socket, url, env var)
// plus anything the driver contributes via engine.Attributer.
func (cfg *Config) attributesFor(p *pendingInstance) (cty.Value, error) {
	addr, err := cfg.InstanceAddr(p.decl)
	if err != nil || addr == "" {
		// No address: either no port yet (parse is lenient — validatePorts reports
		// it clearly) or a portless worker process. Expose identity only; the
		// address-derived attributes appear once there's an endpoint.
		return cty.ObjectVal(map[string]cty.Value{
			"name":   cty.StringVal(p.decl.Name),
			"engine": cty.StringVal(p.decl.Type),
		}), nil
	}
	// In domains mode a reference's user-facing host is the service's DNS name
	// (which resolves to its per-service loopback IP), not a raw address — so
	// `postgres.orders_pg.url` stays correct even when several Postgres share
	// port 5432 on distinct IPs. The raw address is only the daemon's bind target.
	if cfg.Defaults.Domains {
		if host, portStr, err := net.SplitHostPort(addr); err == nil && host != "" {
			addr = net.JoinHostPort(cfg.DomainFor(p.decl.Name), portStr)
		}
	}
	ep := engineEndpoint(addr)
	inst := engine.Instance{
		Name:     p.decl.Name,
		Type:     p.decl.Type,
		Version:  p.decl.Version,
		Endpoint: ep,
		Spec:     p.decl.Spec,
	}
	envVar, url := p.drv.ConnString(inst, ep)
	// AWS built-ins share one port-less endpoint per type; a `<type>.<name>.url`
	// reference resolves to it (http://s3.<stack>.doze), not the internal
	// backend address — the app talks to the shared endpoint and names the
	// bucket/queue/topic via `.name`.
	if shared, ok := cfg.AWSEndpoint(p.decl.Type); ok {
		url = shared
	}
	m := map[string]cty.Value{
		"name":    cty.StringVal(p.decl.Name),
		"engine":  cty.StringVal(p.decl.Type),
		"address": cty.StringVal(addr),
		"url":     cty.StringVal(url),
		"env_var": cty.StringVal(envVar),
	}
	if ep.UnixSocket != "" {
		m["socket"] = cty.StringVal(ep.UnixSocket)
		m["host"] = cty.StringVal("")
		m["port"] = cty.NumberIntVal(0)
	} else if host, portStr, err := net.SplitHostPort(addr); err == nil {
		port, _ := strconv.Atoi(portStr)
		m["host"] = cty.StringVal(host)
		m["port"] = cty.NumberIntVal(int64(port))
		m["socket"] = cty.StringVal("")
	}
	if a, ok := p.drv.(engine.Attributer); ok {
		for k, v := range a.Attributes(inst, ep) {
			m[k] = v
		}
	}
	return cty.ObjectVal(m), nil
}

// engineEndpoint maps a client-facing address ("unix:/path" or "host:port") to
// the engine.Endpoint shape drivers consume.
func engineEndpoint(addr string) engine.Endpoint {
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		return engine.Endpoint{UnixSocket: path}
	}
	return engine.Endpoint{TCPAddr: addr}
}
