package valkey

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"

	"github.com/nerdmenot/doze/internal/engine"
)

// Config is the Valkey-specific configuration decoded from a `valkey` block.
type Config struct {
	// Password, if set, enables AUTH (requirepass).
	Password string
	// Maxmemory caps memory, e.g. "256mb" (empty = unlimited).
	Maxmemory string
}

type vkBody struct {
	Password  string `hcl:"password,optional"`
	Maxmemory string `hcl:"maxmemory,optional"`
}

// DecodeConfig implements engine.ConfigDecoder for the valkey block. It also
// rejects unknown keys (gohcl is strict), so typos surface as config errors.
func (Driver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, _ string) (engine.EngineConfig, error) {
	var raw vkBody
	if d := gohcl.DecodeBody(body, ctx, &raw); d.HasErrors() {
		return nil, fmt.Errorf("%s", d.Error())
	}
	return &Config{Password: raw.Password, Maxmemory: raw.Maxmemory}, nil
}
