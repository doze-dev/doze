package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/nerdmenot/doze/internal/engine"
)

// pendingInstance carries an instance whose engine-agnostic fields are decoded
// but whose driver body is decoded later, in dependency order, by evaluate.
type pendingInstance struct {
	decl     *InstanceDecl
	drv      engine.Driver
	body     hcl.Body         // block body (meta-args stripped), for reference extraction
	remain   hcl.Body         // body minus common fields, for the driver decode
	ctx      *hcl.EvalContext // the context this stamp decodes against (carries each/count)
	defRange hcl.Range
	baseDir  string
}

// evaluate is config's second pass: it derives the cross-instance reference graph
// (each instance's references to other instances build its dependency edges),
// topologically orders the instances, then decodes each driver body against an
// evaluation context that already holds the attributes of every instance it
// references — so `sqs = sqs.jobs.name` resolves to a value and the dependency is
// recorded without any hand-declared DependsOn.
func (cfg *Config) evaluate(parser *hclparse.Parser, pending []*pendingInstance, ctx *hcl.EvalContext) error {
	knownTypes := map[string]bool{}
	for _, t := range engine.Types() {
		knownTypes[t] = true
	}
	byName := map[string]*pendingInstance{}
	for _, p := range pending {
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
		if dec, ok := p.drv.(engine.ConfigDecoder); ok {
			spec, err := dec.DecodeConfig(p.remain, decodeCtx, p.baseDir)
			if err != nil {
				return fmt.Errorf("%s %q: %w", p.decl.Type, p.decl.Name, err)
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

// instanceDeps returns the names of other declared instances referenced by p's
// body. A traversal whose root is an engine type and whose next segment names a
// declared instance (e.g. sqs.jobs) is a reference; an unknown instance name is a
// positioned error. Duplicate and self references are dropped.
func (cfg *Config) instanceDeps(parser *hclparse.Parser, p *pendingInstance, knownTypes map[string]bool) ([]string, error) {
	seen := map[string]bool{}
	var deps []string
	for _, t := range referencedTraversals(p.body) {
		root := t.RootName()
		if !knownTypes[root] {
			continue // var./local./functions are handled elsewhere; not a resource ref
		}
		name, ok := traversalName(t)
		if !ok {
			continue
		}
		if name == p.decl.Name {
			continue // self reference
		}
		if _, declared := cfg.index[name]; !declared {
			rng := t.SourceRange()
			return nil, posErr(parser, rng,
				fmt.Sprintf("reference to undeclared instance %q", root+"."+name),
				fmt.Sprintf("no %s instance named %q is declared", root, name))
		}
		if !seen[name] {
			seen[name] = true
			deps = append(deps, name)
		}
	}
	return deps, nil
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
			if dep := byName[dn]; dep != nil {
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
	if err != nil {
		return cty.NilVal, err
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
