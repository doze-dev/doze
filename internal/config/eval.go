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
	drv          engine.Driver               // nil for a module in the shape pass
	body         hcl.Body                    // block body (meta-args stripped), for reference extraction
	remain       hcl.Body                    // body minus common fields, for the driver decode
	ctx          *hcl.EvalContext            // the context this stamp decodes against (carries each/count)
	explicitDeps map[string]engine.Condition // explicit depends_on: instance name -> condition
	after        *hcl.Attribute              // the block's `after`, nil if unset
	defRange     hcl.Range
	blockLabel   string // the source block's label (the count/for_each base, e.g. "assets")
	baseDir      string
}

// declared is everything a stack declares, by name: what a reference is
// checked against.
type declared struct {
	kind        map[string]string // name -> kind
	moduleTypes map[string]bool   // the engine types of the declared modules
}

func (cfg *Config) declared() *declared {
	d := &declared{kind: map[string]string{}, moduleTypes: map[string]bool{}}
	for _, inst := range cfg.Instances {
		d.kind[inst.Name] = inst.Kind
		if inst.Kind == KindModule {
			d.moduleTypes[inst.Type] = true
		}
	}
	for _, s := range cfg.Services {
		d.kind[s.Name] = KindService
	}
	for _, t := range cfg.Tasks {
		d.kind[t.Name] = KindTask
	}
	return d
}

// names returns the declared names of one kind, sorted.
func (d *declared) names(kind string) []string {
	var out []string
	for n, k := range d.kind {
		if k == kind {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// resolve classifies a traversal. A reference to a declared block returns it
// with ok true. Anything that is not a block reference at all — var.x, local.x,
// each.key — returns ok false. A block reference that points at nothing, or is
// written the old way with the engine type first, is a positioned error.
func (d *declared) resolve(parser *hclparse.Parser, t hcl.Traversal) (Ref, bool, error) {
	root := t.RootName()
	name, hasName := traversalName(t)
	if !isKind(root) {
		// The form doze used to read: <type>.<name>.<attribute>.
		if (d.moduleTypes[root] || root == AppEngine) && hasName {
			kind := KindModule
			if root == AppEngine {
				kind = KindApp
			}
			if k, ok := d.kind[name]; ok {
				kind = k
			}
			return Ref{}, false, posErr(parser, t.SourceRange(),
				"a reference starts with the kind, not the type",
				fmt.Sprintf("write %s.%s in place of %s.%s", kind, name, root, name))
		}
		return Ref{}, false, nil
	}
	if !hasName {
		return Ref{}, false, posErr(parser, t.SourceRange(), "incomplete reference",
			fmt.Sprintf("a reference reads %s.<name>.<attribute>", root))
	}
	switch k, ok := d.kind[name]; {
	case !ok:
		detail := fmt.Sprintf("no %s named %q is declared", root, name)
		if s := nearest(name, d.names(root)); s != "" {
			detail += fmt.Sprintf(". Did you mean %s.%s?", root, s)
		}
		return Ref{}, false, posErr(parser, t.SourceRange(), fmt.Sprintf("reference to undeclared %s %q", root, name), detail)
	case k != root:
		return Ref{}, false, posErr(parser, t.SourceRange(),
			fmt.Sprintf("%q is %s, not %s", name, article(k), article(root)),
			fmt.Sprintf("write %s.%s", k, name))
	}
	return Ref{Kind: root, Name: name}, true, nil
}

func article(kind string) string {
	if kind == KindApp {
		return "an app"
	}
	return "a " + kind
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

// evaluate is config's second pass: it derives the cross-instance reference graph
// (each instance's references to other instances build its dependency edges),
// topologically orders the instances, then decodes each driver body against an
// evaluation context that already holds the attributes of every instance it
// references — so `queue = module.jobs.name` resolves to a value and the
// dependency is recorded without any hand-declared ordering.
func (l *loader) evaluate(cfg *Config, pending []*pendingInstance, ctx *hcl.EvalContext, known *declared) error {
	parser := l.parser
	byName := map[string]*pendingInstance{}
	for _, p := range pending {
		byName[p.decl.Name] = p
	}

	// 1. Reference extraction: each instance's dependency names, validated.
	for _, p := range pending {
		if err := cfg.instanceDeps(parser, p, known); err != nil {
			return err
		}
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
					fmt.Sprintf("%s depends on %q, which is disabled", p.decl.Title(), d.Name),
					fmt.Sprintf("enable %q, or disable %q as well", d.Name, p.decl.Name))
			}
		}
	}

	// 3. Decode each driver body in order, growing the evaluation context (which
	// already holds var.*/local.*, service.* and task.*) with each instance's
	// attribute object under module.<name> or app.<name>. Both roots exist from
	// the start, so a stack with no apps still reads `app.x` as "no such app".
	byKind := map[string]map[string]cty.Value{KindModule: {}, KindApp: {}}
	ctx.Variables[KindModule] = cty.EmptyObjectVal
	ctx.Variables[KindApp] = cty.EmptyObjectVal
	var standIns map[string]map[string]bool
	if l.shape {
		standIns = l.referencedModuleAttrs()
	}
	for _, p := range order {
		// p.ctx is the stamp-specific context (with each/count) for a for_each/count
		// instance; it chains to the shared ctx so var/local/resources resolve too.
		decodeCtx := p.ctx
		if decodeCtx == nil {
			decodeCtx = ctx
		}
		var attrs cty.Value
		if p.drv == nil {
			attrs = standInAttributes(p.decl, standIns[p.decl.Name])
		} else {
			if err := l.decodeBody(cfg, p, ctx, decodeCtx); err != nil {
				return err
			}
			if attrs, err = cfg.attributesFor(p); err != nil {
				return err
			}
		}
		byKind[p.decl.Kind][p.decl.Name] = attrs
		ctx.Variables[p.decl.Kind] = cty.ObjectVal(byKind[p.decl.Kind])
	}
	return nil
}

// decodeBody hands one instance's body to its driver.
func (l *loader) decodeBody(cfg *Config, p *pendingInstance, ctx, decodeCtx *hcl.EvalContext) error {
	switch dec := p.drv.(type) {
	case engine.ConfigDecoder: // in-tree engine
		spec, err := dec.DecodeConfig(p.remain, decodeCtx, p.baseDir, p.decl.Version)
		if err != nil {
			return fmt.Errorf("%s: %w", p.decl.Title(), err)
		}
		// Config reads `port` off the block before the driver sees the rest; an
		// engine whose probes need it takes it here.
		if b, ok := spec.(interface{ BindPort(int) error }); ok {
			if err := b.BindPort(p.decl.Port); err != nil {
				return posErr(l.parser, p.defRange, p.decl.Title()+": "+err.Error(), "")
			}
		}
		p.decl.Spec = spec
	case engine.RemoteDecoder: // out-of-process plugin: it decodes its own block
		src := l.pluginBytes(p.defRange.Filename)
		if src == nil {
			return fmt.Errorf("%s: cannot locate source for remote decode", p.decl.Title())
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
			return fmt.Errorf("%s: %s%s", p.decl.Title(), msg, l.hooks.remoteDecodeErrSuffix(p.decl.Type))
		}
		p.decl.Spec = spec
	}
	return nil
}

// referencedModuleAttrs collects, per module name, every attribute the config
// reads off it (module.db.url → db: url). The shape pass has no module to ask
// what attributes exist, so it stands in exactly the ones that are used.
func (l *loader) referencedModuleAttrs() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, f := range l.files {
		for _, t := range referencedTraversals(f.Body) {
			if t.RootName() != KindModule || len(t) < 3 {
				continue
			}
			name, ok := traversalName(t)
			attr, isAttr := t[2].(hcl.TraverseAttr)
			if !ok || !isAttr {
				continue
			}
			if out[name] == nil {
				out[name] = map[string]bool{}
			}
			out[name][attr.Name] = true
		}
	}
	return out
}

// standInAttributes is what a module publishes in the shape pass: the baseline
// every module has, plus a string for each further attribute the config reads.
// The values are placeholders — only their presence and type matter offline.
func standInAttributes(decl *InstanceDecl, used map[string]bool) cty.Value {
	m := map[string]cty.Value{
		"name":    cty.StringVal(decl.Name),
		"engine":  cty.StringVal(decl.Type),
		"address": cty.StringVal("127.0.0.1:" + strconv.Itoa(decl.Port)),
		"host":    cty.StringVal("127.0.0.1"),
		"port":    cty.NumberIntVal(int64(decl.Port)),
		"url":     cty.StringVal(""),
		"env_var": cty.StringVal(""),
		"socket":  cty.StringVal(""),
	}
	for attr := range used {
		if _, ok := m[attr]; !ok {
			m[attr] = cty.StringVal("")
		}
	}
	return cty.ObjectVal(m)
}

// instanceDeps records what p must boot first: every module or app it
// references (a Healthy dependency), any `after` entry, and any explicit
// depends_on (which may set a stronger or weaker condition). References to
// services and tasks are checked and kept on the declaration, but add no boot
// edge until something runs them. Self references drop.
func (cfg *Config) instanceDeps(parser *hclparse.Parser, p *pendingInstance, known *declared) error {
	cond := map[string]engine.Condition{}
	var order []string
	edge := func(ref Ref) {
		if ref.Name == p.decl.Name || (ref.Kind != KindModule && ref.Kind != KindApp) {
			return
		}
		if _, ok := cond[ref.Name]; !ok {
			order = append(order, ref.Name)
			cond[ref.Name] = engine.Healthy
		}
	}
	// Reference-derived dependencies (always Healthy).
	for _, t := range referencedTraversals(p.body) {
		ref, ok, err := known.resolve(parser, t)
		if err != nil {
			return err
		}
		if !ok || ref.Name == p.decl.Name {
			continue
		}
		p.decl.Refs = append(p.decl.Refs, ref)
		edge(ref)
	}
	if p.after != nil {
		after, err := parseAfter(parser, p.after, known)
		if err != nil {
			return err
		}
		p.decl.After = after
		for _, ref := range after {
			edge(ref)
		}
	}
	// Explicit depends_on conditions (override the default, add new edges).
	for _, name := range sortedConditionKeys(p.explicitDeps) {
		if name == p.decl.Name {
			continue
		}
		if _, declared := cfg.index[name]; !declared {
			return posErr(parser, p.defRange,
				fmt.Sprintf("%s: depends_on references undeclared instance %q", p.decl.Title(), name), "")
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
	p.decl.Deps = deps
	return nil
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
// none. A block's own `after` is left out: it names blocks to wait for, and is
// read by parseAfter.
func referencedTraversals(body hcl.Body) []hcl.Traversal {
	hb, ok := body.(*hclsyntax.Body)
	if !ok {
		return nil
	}
	var out []hcl.Traversal
	var walk func(b *hclsyntax.Body)
	walk = func(b *hclsyntax.Body) {
		for name, attr := range b.Attributes {
			if b == hb && name == "after" {
				continue
			}
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
	// `module.orders_pg.url` stays correct even when several Postgres share
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
