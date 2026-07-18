// HCL decode targets and decoders for the fixed root blocks (defaults, tls,
// modules) and the common fields config reads from every engine block.
package config

import (
	"fmt"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

// common is the partial-decode target for the fields config reads from every
// engine block; the rest of the body goes to the driver.
type common struct {
	Version string   `hcl:"version,optional"`
	Listen  string   `hcl:"listen,optional"`
	Port    int      `hcl:"port,optional"`
	Remain  hcl.Body `hcl:",remain"`
}

type hclTLS struct {
	Cert     string `hcl:"cert,optional"`
	Key      string `hcl:"key,optional"`
	Required bool   `hcl:"required,optional"`
}

type hclDefaults struct {
	IdleTimeout string `hcl:"idle_timeout,optional"`
	Domains     bool   `hcl:"domains,optional"`
}

type hclModules struct {
	Mirror  string      `hcl:"mirror,optional"`
	Enabled bool        `hcl:"enabled,optional"`
	Modules []hclModule `hcl:"module,block"`
}

type hclModule struct {
	Name   string `hcl:"name,label"`
	Source string `hcl:"source,optional"`
	// Version pins the MODULE (plugin) release exactly — the escape hatch for
	// holding back a regressed release. Not the engine version: that stays
	// `version =` on the instance block. Exact only, no ranges.
	Version string `hcl:"version,optional"`
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
	cfg.Defaults.Domains = d.Domains
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

func (cfg *Config) decodeModules(parser *hclparse.Parser, block *hcl.Block, ctx *hcl.EvalContext) error {
	var m hclModules
	if diags := gohcl.DecodeBody(block.Body, ctx, &m); diags.HasErrors() {
		return diagError(parser, diags)
	}
	mc := ModulesConfig{
		Mirror:   m.Mirror,
		Enabled:  m.Enabled || m.Mirror != "",
		Sources:  map[string]string{},
		Versions: map[string]string{},
	}
	for _, md := range m.Modules {
		mc.Sources[md.Name] = md.Source
		mc.Versions[md.Name] = md.Version
	}
	cfg.Modules = mc
	return nil
}
