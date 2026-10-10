package daemon

import (
	"fmt"
	"net"

	names "github.com/doze-dev/doze-names"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/endpoints"
)

// bindPlan decides, for one daemon run, where each proxied instance listens and
// what its DNS name resolves to. In loopback mode (defaults{domains=true} and
// the machine set up) every service gets an address of its own and keeps its
// declared port — so many services share a canonical port, disambiguated by IP.
// Otherwise services listen on 127.0.0.1 with distinct ports (today's behavior).
type bindPlan struct {
	bind     map[string]string // instance name -> "ip:port" to proxy.Listen
	resolve  map[string]net.IP // domain -> IP (127.0.0.1 default for unknown in-zone)
	loopback bool
}

// buildBindPlan assigns addresses for every enabled, proxied endpoint. In
// loopback mode each address comes from the shared names registry (zone), which
// is the one allocator for the machine: doze, doze-aws and doze-kafka all take
// addresses from it under one lock, so no two of them can hand out the same one.
func (d *Daemon) buildBindPlan(eps []endpoints.Endpoint, zone *names.Registry) (*bindPlan, error) {
	p := &bindPlan{bind: map[string]string{}, resolve: map[string]net.IP{}}
	domainsOn := d.cfg.Defaults.Domains
	p.loopback = domainsOn && zone != nil && names.PoolUsable()
	loopbackIP := net.IPv4(127, 0, 0, 1)

	usedPort := map[string]string{} // port -> instance, for the fallback dup-port error
	for _, ep := range eps {
		_, port, err := net.SplitHostPort(ep.Address)
		if err != nil {
			continue // unix socket or portless — no TCP listener to place
		}
		decl := d.cfg.Lookup(ep.Name)
		if decl == nil || !decl.Enabled {
			continue
		}

		// An app that binds its own port (`direct = true`) does so on 127.0.0.1,
		// and doze places no listener for it: its name just resolves to loopback.
		if drv, ok := engine.Lookup(ep.Engine); ok && !fronted(drv, engine.Instance{Name: ep.Name, Type: ep.Engine, Spec: decl.Spec}) {
			if ep.Domain != "" {
				p.resolve[ep.Domain] = loopbackIP
			}
			continue
		}

		var ip net.IP
		switch {
		case p.loopback:
			a, err := claimAddress(zone, d.cfg.DomainFor(ep.Name))
			if err != nil {
				return nil, err
			}
			ip = a
		default:
			ip = loopbackIP
			if domainsOn { // wanted per-IP but the machine is not set up
				if other, dup := usedPort[port]; dup {
					return nil, fmt.Errorf(
						"%q and %q both use port %s, but this machine is not set up to give each service its own address — run `doze doctor --fix` once (needs sudo), or give them distinct ports",
						other, ep.Name, port)
				}
				usedPort[port] = ep.Name
			}
		}
		p.bind[ep.Name] = net.JoinHostPort(ip.String(), port)
		if ep.Domain != "" {
			p.resolve[ep.Domain] = ip
		}
	}
	return p, nil
}

// claimAddress claims host in the shared registry and returns the address the
// registry gave it. Claiming is allocating: the name and its address are
// recorded together, under the registry's lock. A name another live process
// holds is an error that says who.
func claimAddress(zone *names.Registry, host string) (net.IP, error) {
	lease, err := zone.Claim(names.Name{Host: host, Tier: names.TierQualified})
	if err != nil {
		if held, ok := names.Held(err); ok {
			return nil, fmt.Errorf("%s is already in use on this machine by %s (pid %d): stop it, or rename this stack or service",
				held.Host, held.Owner, held.PID)
		}
		return nil, fmt.Errorf("no address for %s: %w", host, err)
	}
	return lease.IP, nil
}

// fronted reports whether doze holds an instance's declared port with a proxy
// listener of its own. That is every module, and every app that declares a
// port and has not set `direct`. What is left — a portless worker, a direct
// app — binds for itself or not at all.
func fronted(drv engine.Driver, inst engine.Instance) bool {
	if lc, ok := drv.(engine.Lifecycle); ok && lc.Supervised(inst) {
		px, ok := drv.(interface{ Proxied(engine.Instance) bool })
		return ok && px.Proxied(inst)
	}
	return true
}
