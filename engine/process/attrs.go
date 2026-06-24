package process

import (
	"fmt"

	"github.com/zclconf/go-cty/cty"

	"github.com/nerdmenot/doze/internal/engine"
)

// Attributes implements engine.Attributer: expose the app's own listen address so
// other instances can reference process.<name>.{url,host,port}. These override the
// generic baseline (which would otherwise hold a doze proxy address this process
// does not use).
func (Driver) Attributes(inst engine.Instance, _ engine.Endpoint) map[string]cty.Value {
	cfg, ok := inst.Spec.(*Config)
	if !ok || cfg.Port == 0 {
		return nil
	}
	host := "127.0.0.1"
	return map[string]cty.Value{
		"host": cty.StringVal(host),
		"port": cty.NumberIntVal(int64(cfg.Port)),
		"url":  cty.StringVal(fmt.Sprintf("http://%s:%d", host, cfg.Port)),
	}
}
