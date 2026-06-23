package valkey

import (
	"fmt"
	"sort"

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
	// MaxmemoryPolicy is the eviction policy (e.g. "allkeys-lru").
	MaxmemoryPolicy string
	// Appendonly enables the AOF persistence log.
	Appendonly bool
	// Save is the RDB snapshot schedule (e.g. "3600 1 300 100"); empty disables it.
	Save string
	// Settings is a raw valkey.conf passthrough for any directive doze does not
	// model with a typed field (e.g. {"maxmemory-policy" = "allkeys-lru"}).
	Settings map[string]string
}

type vkBody struct {
	Password        string            `hcl:"password,optional"`
	Maxmemory       string            `hcl:"maxmemory,optional"`
	MaxmemoryPolicy string            `hcl:"maxmemory_policy,optional"`
	Appendonly      bool              `hcl:"appendonly,optional"`
	Save            *string           `hcl:"save,optional"`
	Settings        map[string]string `hcl:"settings,optional"`
}

// DecodeConfig implements engine.ConfigDecoder for the valkey block. It also
// rejects unknown keys (gohcl is strict), so typos surface as config errors.
func (Driver) DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, _ string) (engine.EngineConfig, error) {
	var raw vkBody
	if d := gohcl.DecodeBody(body, ctx, &raw); d.HasErrors() {
		return nil, fmt.Errorf("%s", d.Error())
	}
	c := &Config{
		Password:        raw.Password,
		Maxmemory:       raw.Maxmemory,
		MaxmemoryPolicy: raw.MaxmemoryPolicy,
		Appendonly:      raw.Appendonly,
		Settings:        raw.Settings,
	}
	if raw.Save != nil {
		c.Save = *raw.Save
	}
	return c, nil
}

// sortedKeys returns the keys of m in deterministic order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
