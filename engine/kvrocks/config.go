package kvrocks

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"

	"github.com/nerdmenot/doze/internal/engine"
)

// Config is the Kvrocks-specific configuration decoded from a `kvrocks` block.
type Config struct {
	// Password, if set, enables AUTH (requirepass).
	Password string
	// Workers is the size of the worker thread pool (0 = kvrocks default).
	Workers int
	// Namespaces are kvrocks namespaces, each with an access token.
	Namespaces []Namespace
	// Settings is a raw kvrocks.conf passthrough for any directive doze does not
	// model with a typed field (e.g. {"rocksdb.block_size" = "16384"}).
	Settings map[string]string
}

// Namespace is a kvrocks namespace and its access token.
type Namespace struct {
	Name  string
	Token string
}

type kvBody struct {
	Password   string            `hcl:"password,optional"`
	Workers    int               `hcl:"workers,optional"`
	Namespaces []kvNamespace     `hcl:"namespace,block"`
	Settings   map[string]string `hcl:"settings,optional"`
}

type kvNamespace struct {
	Name  string `hcl:"name,label"`
	Token string `hcl:"token"`
}

// DecodeConfig implements engine.ConfigDecoder for the kvrocks block.
func (Driver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, _ string) (engine.EngineConfig, error) {
	var raw kvBody
	if d := gohcl.DecodeBody(body, ctx, &raw); d.HasErrors() {
		return nil, fmt.Errorf("%s", d.Error())
	}
	c := &Config{Password: raw.Password, Workers: raw.Workers, Settings: raw.Settings}
	seen := map[string]bool{}
	for _, ns := range raw.Namespaces {
		if ns.Name == "" {
			return nil, fmt.Errorf("kvrocks namespace needs a name")
		}
		if seen[ns.Name] {
			return nil, fmt.Errorf("kvrocks namespace %q is declared more than once", ns.Name)
		}
		seen[ns.Name] = true
		c.Namespaces = append(c.Namespaces, Namespace{Name: ns.Name, Token: ns.Token})
	}
	// Namespaces require a requirepass (the default-namespace token) to be set.
	if len(c.Namespaces) > 0 && c.Password == "" {
		return nil, fmt.Errorf("kvrocks namespaces require a `password` (the default-namespace token)")
	}
	return c, nil
}
