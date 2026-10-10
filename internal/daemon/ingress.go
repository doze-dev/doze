// The HTTP front door: an app that declares a `port` is also served at
// http://<name>.<stack>.doze, with no port, through the one :80 listener every
// doze program on the machine shares (doze-names). This file only says which
// names this stack wants fronted and where each goes; the listener, and the
// table it routes from, belong to the shared registry — so whichever of doze,
// doze-aws and doze-kafka started first serves all of them, from one file.
package daemon

import (
	"context"
	"errors"
	"net"
	"syscall"

	names "github.com/doze-dev/doze-names"

	"github.com/doze-dev/doze/internal/endpoints"
)

// IngressPort is the front door's port.
const IngressPort = 80

// setupIngress publishes this stack's routes and joins the shared front door.
// awsRoutes are the stack's local-AWS hosts (see awsingress.go).
func (d *Daemon) setupIngress(ctx context.Context, eps []endpoints.Endpoint, plan *bindPlan, awsRoutes map[string]string) (release func(), err error) {
	if !d.cfg.Defaults.Domains || d.zone == nil {
		return func() {}, nil // no names, so nothing to front
	}
	d.publishRoutes(eps, plan, awsRoutes)
	front := names.ServeIngress(ctx, d.zone, d.logf)
	return front.Close, nil
}

// publishRoutes writes this stack's routes into the shared registry. A route
// lives on its name's entry, so it disappears with the name: when a service is
// removed, or when this daemon dies.
func (d *Daemon) publishRoutes(eps []endpoints.Endpoint, plan *bindPlan, awsRoutes map[string]string) {
	routes := d.ingressRoutes(eps, plan)
	for host, target := range awsRoutes {
		routes[host] = target
	}
	for host, target := range routes {
		if err := d.zone.RouteFor(host, target); err != nil {
			// The usual reason is that the name is someone else's: a standalone
			// tool holding aws.<stack>.doze. status and doctor say who.
			d.logf("front door: %s is not routed: %v", host, err)
			continue
		}
		d.logf("front door: http://%s → %s", host, target)
	}
}

// ingressRoutes derives this stack's Host → target table: every enabled app
// that doze fronts. The target is the app's public address — doze's own
// listener — so a request through the front door wakes a sleeping app exactly
// as a direct connection does.
func (d *Daemon) ingressRoutes(eps []endpoints.Endpoint, plan *bindPlan) map[string]string {
	byName := map[string]endpoints.Endpoint{}
	for _, ep := range eps {
		byName[ep.Name] = ep
	}
	routes := map[string]string{}
	for _, decl := range d.cfg.Instances {
		if !decl.Enabled {
			continue
		}
		fwd, ok := decl.Spec.(interface{ ForwardPort() int })
		if !ok || fwd.ForwardPort() <= 0 {
			continue
		}
		ep, ok := byName[decl.Name]
		if !ok || ep.Domain == "" {
			continue
		}
		target := ep.Address
		if b := plan.bind[decl.Name]; b != "" {
			target = b
		}
		routes[ep.Domain] = target
	}
	return routes
}

func isAddrInUse(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) {
		return errors.Is(op.Err, syscall.EADDRINUSE)
	}
	return errors.Is(err, syscall.EADDRINUSE)
}
