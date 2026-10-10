// Local DNS names for services: with defaults{domains=true} every enabled TCP
// endpoint gets <service>.<stack>.doze, answered by the built-in
// resolver (resolver.go) — so connection strings read as the service
// (postgres://…@orders-pg.demo.doze:5432) instead of a loopback
// address. The stack name is claimed machine-wide (stacks.go) so two projects
// can't shadow each other's names.
package daemon

import (
	"context"
	"net"

	names "github.com/doze-dev/doze-names"

	"github.com/doze-dev/doze/internal/config"
)

// setupDomains publishes this stack's name→IP mappings to the shared registry
// (the stack's own name is claimed in Run, before any address is) and starts
// (or joins) the *.doze resolver —
// answering each name with its per-service loopback IP, from the machine-wide
// union so any daemon can resolve any stack. Returns a release func for shutdown.
func (d *Daemon) setupDomains(ctx context.Context, own map[string]net.IP) (release func(), err error) {
	if !d.cfg.Defaults.Domains {
		return func() {}, nil
	}
	stack := d.cfg.Stack()

	// Publish our names into the SHARED registry — the same one doze-aws and
	// doze-kafka write to. A proxied service's name is already there, claimed
	// with its address when the bind plan was built; claiming it again here
	// refreshes it. What this adds are the names with no address of their own:
	// apps, which resolve to 127.0.0.1.
	reg := d.zone
	leases := make([]*names.Lease, 0, len(own))
	for host, ip := range own {
		l, err := reg.ClaimAt(names.Name{Host: host, Tier: names.TierQualified}, ip)
		if err != nil {
			if held, ok := names.Held(err); ok {
				d.logf("domains: %s is held by pid %d (%s)", held.Host, held.PID, held.Owner)
			} else {
				d.logf("domains: could not register %s: %v", host, err)
			}
			continue
		}
		leases = append(leases, l)
	}
	d.zoneLeases = leases
	release = func() {
		for _, l := range leases {
			_ = l.Release()
		}
	}
	_ = leases
	resolve := func(name string) net.IP {
		// own is the live bind-plan resolve map, which live Add/Remove mutates
		// under d.mu — lock so a concurrent add can't race the DNS read. It is
		// consulted first so a service added to a running stack answers at once,
		// then the registry, which carries every peer's names too.
		d.mu.Lock()
		ip, ok := own[name]
		d.mu.Unlock()
		if ok {
			return ip
		}
		return reg.Resolve(name)
	}

	// The unicast resolver on 127.0.0.1:5323 backs the /etc/resolver drop-in
	// (macOS) or the resolver zone you point at it (Linux). Only one daemon binds
	// it; via the shared registry above it answers for every stack. We no longer
	// run an mDNS responder: the suffix is a plain unicast domain, not .local, so
	// there's no multicast path — and mDNS pressured macOS's mDNSResponder.
	srv := names.ServeResolve(ctx, resolve, d.logf)
	prev := release
	release = func() { prev(); srv.Close() }
	d.logf("domains: *.%s.%s → per-service IP (zone on %s)", stack, config.DomainSuffix, names.ResolverAddr())

	if !names.Check().OK() {
		d.logf("domains: names will not resolve through the system resolver until you run `doze doctor --fix` (one sudo)")
	}
	return release, nil
}
