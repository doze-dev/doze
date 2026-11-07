// Load-path validation that bare Parse skips.
package config

import (
	"errors"
	"fmt"
	"sort"
)

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// validatePorts enforces the explicit-port rule for a loaded config: every enabled
// instance must resolve to a client-facing address, and no two may share one. It
// runs on the CLI load path (not bare Parse) so a missing port or a collision is a
// clear, named error — never a silent auto-assignment or an opaque bind failure.
//
// When defaults{names=true}, the port-uniqueness check is DROPPED: each service
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
		uses := map[string]string{addr: fmt.Sprintf("%q", decl.Name)}
		for a, what := range cfg.extraListenerAddrs(decl) {
			uses[a] = what
		}
		for _, a := range sortedKeys(uses) {
			if other, dup := addrs[a]; dup {
				errs = append(errs, fmt.Errorf("port conflict: %s and %s both use %s — give each instance a unique port (or `defaults { names = true }` to share ports by name)", other, uses[a], a))
				continue
			}
			addrs[a] = uses[a]
		}
	}
	return errors.Join(errs...)
}
