package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	ctyjson "github.com/zclconf/go-cty/cty/json"

	"github.com/doze-dev/doze-sdk/engine"

	_ "github.com/doze-dev/doze/engine/process" // the built-in engine of app, service and task
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/fault"
)

// moduleTypes get a generic driver: its spec is the block body evaluated
// as written (attributes and nested blocks), which is what doze-config hands a
// module's declared schema in the Rust port. "fake" is the Go tests' type.
var moduleTypes = []string{
	"fake", "postgres", "mariadb", "valkey", "kvrocks", "temporal", "ferret", "kafka", "standin",
}

func init() {
	for _, t := range moduleTypes {
		engine.Register(genericDriver{t})
	}
	for _, t := range versionlessTypes {
		engine.Register(versionlessDriver{genericDriver{t}})
	}
}

// versionlessTypes take no `version` (the real modules say so the same way).
var versionlessTypes = []string{"aws", "httpd"}

type versionlessDriver struct{ genericDriver }

func (versionlessDriver) Versionless() {}

// configAll loads each path as doze does and writes one line per path.
func configAll(args []string) {
	shape := false
	if len(args) > 0 && args[0] == "-shape" {
		shape, args = true, args[1:]
	}
	for _, p := range args {
		emit(configOne(p, shape))
	}
}

func configOne(path string, shape bool) map[string]any {
	var seen []hcl.Diagnostics
	config.OracleDiags = func(d hcl.Diagnostics) { seen = append(seen, d) }
	defer func() { config.OracleDiags = nil }()
	load := func() (*config.Config, error) { return config.Load(path, nil) }
	if shape {
		load = func() (*config.Config, error) { return config.LoadShape(path, nil) }
	}
	cfg, err := load()
	r := map[string]any{"id": filepath.Base(path)}
	if err != nil {
		r["ok"] = false
		r["kind"] = string(fault.Of(err))
		r["err"] = err.Error()
		ds := []map[string]any{}
		for _, d := range seen {
			ds = append(ds, diagsOf(d)...)
		}
		r["diags"] = ds
		return r
	}
	r["ok"] = true
	raw, err := json.Marshal(cfg)
	must(err)
	r["config"] = json.RawMessage(raw)
	return r
}

type genericDriver struct{ ty string }

// genericSpec is a module body as written: attribute values with their
// types, and nested blocks in order.
type genericSpec struct {
	Attrs  map[string]any `json:"attrs"`
	Blocks []genericBlock `json:"blocks"`
}

type genericBlock struct {
	Type   string       `json:"type"`
	Labels []string     `json:"labels"`
	Body   *genericSpec `json:"body"`
}

func (g genericDriver) Type() string { return g.ty }
func (genericDriver) Resolve(context.Context, engine.VersionSpec, engine.Platform, engine.Locker, engine.Fetcher) (engine.Toolchain, error) {
	return engine.Toolchain{}, nil
}
func (genericDriver) Provision(context.Context, engine.Instance, engine.Toolchain) error { return nil }
func (genericDriver) Provisioned(string) bool                                            { return false }
func (genericDriver) BackendSocket(string, int) string                                   { return "" }
func (genericDriver) ConnString(engine.Instance, engine.Endpoint) (string, string)       { return "", "" }

func (genericDriver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, _ string, _ engine.VersionSpec) (engine.EngineConfig, error) {
	sb, ok := body.(*hclsyntax.Body)
	if !ok {
		return nil, errors.New("oracle-dump: module body is not hclsyntax")
	}
	spec, diags := decodeGeneric(sb, ctx)
	if diags.HasErrors() {
		return nil, diags
	}
	return spec, nil
}

// decodeGeneric reads what PartialContent left visible: hclsyntax keeps the
// names config consumed in unexported hidden sets, read here by reflection.
func decodeGeneric(b *hclsyntax.Body, ctx *hcl.EvalContext) (*genericSpec, hcl.Diagnostics) {
	hiddenA, hiddenB := hidden(b, "hiddenAttrs"), hidden(b, "hiddenBlocks")
	spec := &genericSpec{Attrs: map[string]any{}, Blocks: []genericBlock{}}
	var diags hcl.Diagnostics
	names := make([]string, 0, len(b.Attributes))
	for n := range b.Attributes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if hiddenA[n] {
			continue
		}
		v, d := b.Attributes[n].Expr.Value(ctx)
		diags = append(diags, d...)
		if d.HasErrors() {
			continue
		}
		ty, err := ctyjson.MarshalType(v.Type())
		must(err)
		spec.Attrs[n] = map[string]any{"value": canon(v), "type": json.RawMessage(ty)}
	}
	for _, bl := range b.Blocks {
		if hiddenB[bl.Type] {
			continue
		}
		inner, d := decodeGeneric(bl.Body, ctx)
		diags = append(diags, d...)
		spec.Blocks = append(spec.Blocks, genericBlock{Type: bl.Type, Labels: bl.Labels, Body: inner})
	}
	return spec, diags
}

func hidden(b *hclsyntax.Body, field string) map[string]bool {
	out := map[string]bool{}
	m := reflect.ValueOf(b).Elem().FieldByName(field)
	if !m.IsValid() || m.IsNil() {
		return out
	}
	for _, k := range m.MapKeys() {
		out[k.String()] = true
	}
	return out
}
