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
}

type kvBody struct {
	Password string `hcl:"password,optional"`
}

// DecodeConfig implements engine.ConfigDecoder for the kvrocks block.
func (Driver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, _ string) (engine.EngineConfig, error) {
	var raw kvBody
	if d := gohcl.DecodeBody(body, ctx, &raw); d.HasErrors() {
		return nil, fmt.Errorf("%s", d.Error())
	}
	return &Config{Password: raw.Password}, nil
}
