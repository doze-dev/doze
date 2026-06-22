package ferretdb

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"

	"github.com/nerdmenot/doze/internal/engine"
)

// Config is the FerretDB-specific configuration decoded from a `ferretdb` block.
type Config struct {
	// Backend is the name of the declared postgres instance FerretDB stores data
	// in. FerretDB v2 keeps all state in PostgreSQL (with the documentdb
	// extension), so this is required.
	Backend string
}

type fdBody struct {
	Backend string `hcl:"backend"` // required
}

// DecodeConfig implements engine.ConfigDecoder for the ferretdb block.
func (Driver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, _ string) (engine.EngineConfig, error) {
	var raw fdBody
	if d := gohcl.DecodeBody(body, ctx, &raw); d.HasErrors() {
		return nil, fmt.Errorf("%s", d.Error())
	}
	if raw.Backend == "" {
		return nil, fmt.Errorf("ferretdb requires a `backend` (the name of a postgres instance)")
	}
	return &Config{Backend: raw.Backend}, nil
}
