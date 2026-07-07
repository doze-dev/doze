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

	// Publish our names into the shared registry so whichever daemon owns the
	// unicast resolver can answer for every stack on the machine; the resolver
	// consults our in-memory map first, then that shared union.
	pid := os.Getpid()
	publishDomains(d.cfg.Home, own, pid)
	release = func() { unclaim(); unpublishDomains(d.cfg.Home, pid) }
	resolve := func(name string) net.IP {
		if ip, ok := own[name]; ok {
			return ip
		}
		return sharedResolve(d.cfg.Home, name)
	}

	// The unicast resolver on 127.0.0.1:5323 backs the /etc/resolver drop-in
	// (macOS) or the resolver zone you point at it (Linux). Only one daemon binds
	// it; via the shared registry above it answers for every stack. We no longer
	// run an mDNS responder: the suffix is a plain unicast domain, not .local, so
	// there's no multicast path — and mDNS pressured macOS's mDNSResponder.
	bound, dnsErr := serveDNS(ctx, resolve)
	switch {
	case dnsErr != nil:
		d.logf("domains: resolver failed to start: %v", dnsErr)
	case bound:
		d.logf("domains: *.%s.%s → per-service IP (resolver on 127.0.0.1:%d)", stack, config.DomainSuffix, DNSPort)
	default:
		d.logf("domains: *.%s.%s → per-service IP (resolver served by another stack's daemon)", stack, config.DomainSuffix)
	}

	if !ResolverConfigured() {
		if runtime.GOOS == "darwin" {
			d.logf("domains: names won't resolve until you run `doze dns-setup` (one sudo)")
		} else {
			d.logf("domains: point your resolver's %s zone at 127.0.0.1:%d (systemd-resolved or dnsmasq)", config.DomainSuffix, DNSPort)
		}
	}
	return release, nil
}
