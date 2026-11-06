// Local DNS names for services: with defaults{names=true} every enabled TCP
// endpoint gets <service>.<stack>.doze, answered by the built-in
// resolver (resolver.go) — so connection strings read as the service
// (postgres://…@orders-pg.demo.doze:5432) instead of a loopback
// address. The stack name is claimed machine-wide (stacks.go) so two projects
// can't shadow each other's names.
package daemon

import (
	"context"
	"fmt"
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
				// Not fatal — the stack runs, and that one name is someone else's —
				// but it must not be silent: status and doctor say who has it.
				d.noteHeld(held)
			} else {
				d.logf("domains: could not register %s: %v", host, err)
			}
			continue
		}
		leases = append(leases, l)
		leases = append(leases, d.claimAppDomains(l, host)...)
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
	srv := names.ServeResolveIn(ctx, reg, resolve, d.logf)
	prev := release
	release = func() { prev(); srv.Close() }
	d.logf("domains: *.%s.%s → per-service IP (zone on %s)", stack, config.DomainSuffix, names.ResolverAddr())

	if !names.Check().OK() {
		d.logf("domains: this machine's setup for names is missing or out of date — `doze doctor` says which part, `doze doctor --fix` does it (one sudo)")
	}
	return release, nil
}

// noteHeld records that a name this stack wanted belongs to another program,
// against the instance the name is for.
func (d *Daemon) noteHeld(held *names.ErrHeld) {
	who := fmt.Sprintf("%s is held by %s (pid %d)", held.Host, held.Owner, held.PID)
	awsHost := "aws." + d.cfg.Stack() + "." + config.DomainSuffix
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.heldNames == nil {
		d.heldNames = map[string]string{}
	}
	for _, decl := range d.cfg.Decls() {
		if d.cfg.DomainFor(decl.Name) == held.Host || (held.Host == awsHost && decl.Type == config.AWSUnifiedType) {
			d.heldNames[decl.Name] = who
		}
		for _, dom := range decl.Domains {
			if dom == held.Host {
				d.heldNames[decl.Name] = who // another stack declares this domain
			}
		}
	}
}

// claimAppDomains twins an app's own domains (dev.acme.com, *.dev.acme.com)
// onto the lease of its doze name, so they resolve to the same address and
// go away with it; a certificate the person brought is recorded on them. A
// domain another live stack holds is logged and skipped, as a held name is.
func (d *Daemon) claimAppDomains(l *names.Lease, host string) []*names.Lease {
	var out []*names.Lease
	for _, decl := range d.cfg.Decls() {
		if !decl.Enabled || len(decl.Domains) == 0 || d.cfg.DomainFor(decl.Name) != host {
			continue
		}
		for _, dom := range decl.Domains {
			t, err := l.Twin(names.DomainName(dom))
			if err != nil {
				if held, ok := names.Held(err); ok {
					d.logf("domains: %s is held by pid %d (%s)", held.Host, held.PID, held.Owner)
					d.noteHeld(held)
				} else {
					d.logf("domains: could not register %s: %v", dom, err)
				}
				continue
			}
			if decl.Cert != "" {
				if err := t.Certificate(decl.Cert, decl.Key); err != nil {
					d.logf("domains: %s: %v", dom, err)
				}
			}
			out = append(out, t)
		}
	}
	return out
}

// usesDozeCA says some app declares a domain without bringing its own
// certificate: the front door mints one, and the stack's processes are
// told where the authority is so they can verify it.
func (d *Daemon) usesDozeCA() bool {
	for _, decl := range d.cfg.Decls() {
		if decl.Enabled && len(decl.Domains) > 0 && decl.Cert == "" {
			return true
		}
	}
	return false
}
