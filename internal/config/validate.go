// Load-path validation that bare Parse skips.
package config

import (
	"errors"
	"fmt"
)

// validatePorts enforces the explicit-port rule for a loaded config: every enabled
// instance must resolve to a client-facing address, and no two may share one. It
// runs on the CLI load path (not bare Parse) so a missing port or a collision is a
// clear, named error — never a silent auto-assignment or an opaque bind failure.
//
// When defaults{domains=true}, the port-uniqueness check is DROPPED: each service
// gets its own loopback IP (127.0.0.x, see `doze dns-setup`), so many services
// share the same canonical port — every Postgres on 5432, disambiguated by name.
// A must-still-have-a-port check stays; the daemon reports clearly if the loopback
// range isn't set up when a duplicate port is actually declared.
func (cfg *Config) validatePorts() error {
	addrs := map[string]string{} // address -> instance name
	var errs []error             // every violation, not just the first — one fix cycle, not N
	for _, decl := range cfg.Instances {
		if !decl.Enabled {
			continue // paused instances bind nothing
		}
		addr, err := cfg.InstanceAddr(decl)
		if err != nil {
			errs = append(errs, err) // "<type> "<name>" has no port — add `port = NNNN`"
			continue
		}
		if addr == "" {
			continue // a portless process (worker) binds nothing — can't collide
		}
		if cfg.Defaults.Domains {
			continue // per-service loopback IPs disambiguate; ports may repeat
		}
		if other, dup := addrs[addr]; dup {
			errs = append(errs, fmt.Errorf("port conflict: %q and %q both use %s — give each instance a unique port (or `defaults { domains = true }` to share ports by name)", other, decl.Name, addr))
			continue
		}
		addrs[addr] = decl.Name
	}
	return errors.Join(errs...)
}
