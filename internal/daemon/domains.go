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
	"os"
	"path/filepath"
	"runtime"

	names "github.com/doze-dev/doze-names"

	"github.com/doze-dev/doze/internal/config"
)

// setupDomains claims the stack name, publishes this stack's name→IP mappings
// to the shared registry, and starts (or joins) the *.doze resolver —
// answering each name with its per-service loopback IP, from the machine-wide
// union so any daemon can resolve any stack. Returns a release func for shutdown.
func (d *Daemon) setupDomains(ctx context.Context, own map[string]net.IP) (release func(), err error) {
	if !d.cfg.Defaults.Domains {
		return func() {}, nil
	}
	stack := d.cfg.Stack()
	dir := filepath.Dir(d.cfg.Path())
	unclaim, err := claimStack(d.cfg.Home, stack, dir, os.Getpid())
	if err != nil {
		return nil, err
	}

	// Publish our names into the SHARED registry — the same one doze-aws and
	// doze-kafka write to. Before this, doze kept its own domains.json and its
	// own resolver on the same port as theirs, so whichever binary started
	// first served the zone and the others' names silently stopped resolving.
	reg := names.Open(d.cfg.Home, "doze")
	d.zone = reg
	leases := make([]*names.Lease, 0, len(own))
	for host, ip := range own {
		// ClaimAt, not Claim: these addresses come from doze's own allocator,
		// which persists them per (stack, service).
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
		unclaim()
		for _, l := range leases {
			_ = l.Release()
		}
	}
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

	if !ResolverConfigured() {
		if runtime.GOOS == "darwin" {
			d.logf("domains: names won't resolve until you run `doze dns-setup` (one sudo)")
		} else {
			d.logf("domains: point your resolver's %s zone at %s (systemd-resolved or dnsmasq)", config.DomainSuffix, names.ResolverAddr())
		}
	}
	return release, nil
}
