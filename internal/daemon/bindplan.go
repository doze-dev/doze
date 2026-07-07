package daemon

import (
	"fmt"
	"net"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/endpoints"
	"github.com/doze-dev/doze/internal/loopback"
)

// bindPlan decides, for one daemon run, where each proxied instance listens and
// what its DNS name resolves to. In loopback mode (defaults{domains=true} and
// the aliased range present) every service gets its own 127.0.0.x and keeps its
// declared port — so many services share a canonical port, disambiguated by IP.
// Otherwise services listen on 127.0.0.1 with distinct ports (today's behavior).
type bindPlan struct {
	bind     map[string]string // instance name -> "ip:port" to proxy.Listen
	resolve  map[string]net.IP // domain -> IP (127.0.0.1 default for unknown in-zone)
	loopback bool
}

// buildBindPlan assigns addresses for every enabled, proxied endpoint.
func (d *Daemon) buildBindPlan(eps []endpoints.Endpoint, alloc *loopback.Allocator) (*bindPlan, error) {
	p := &bindPlan{bind: map[string]string{}, resolve: map[string]net.IP{}}
	domainsOn := d.cfg.Defaults.Domains
	p.loopback = domainsOn && loopback.Available()
	stack := d.cfg.Stack()
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

		// Supervised processes bind their own port on 127.0.0.1; doze never
		// proxies them, so there's no per-IP listener to place — their name just
		// resolves to loopback (the :80 ingress fronts the HTTP ones).
		if drv, ok := engine.Lookup(ep.Engine); ok {
			if lc, isLC := drv.(engine.Lifecycle); isLC &&
				lc.Supervised(engine.Instance{Name: ep.Name, Type: ep.Engine, Spec: decl.Spec}) {
				if ep.Domain != "" {
					p.resolve[ep.Domain] = loopbackIP
				}
				continue
			}
		}

		var ip net.IP
		switch {
		case p.loopback:
			a, err := alloc.For(stack, ep.Name)
			if err != nil {
				return nil, err
			}
			ip = a
		default:
			ip = loopbackIP
			if domainsOn { // wanted per-IP but the range isn't aliased
				if other, dup := usedPort[port]; dup {
					return nil, fmt.Errorf(
						"%q and %q both use port %s, but per-service addressing isn't set up — run `doze dns-setup` once (needs sudo) to give each its own address, or give them distinct ports",
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
