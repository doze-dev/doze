// The HTTP front door: an app that declares a `port` is also served at
// http://<name>.<stack>.doze, with no port, through the one :80 listener every
// doze program on the machine shares (doze-names). This file only says which
// names this stack wants fronted and where each goes; the listener, and the
// table it routes from, belong to the shared registry — so whichever of doze,
// doze-aws and doze-kafka started first serves all of them, from one file.
package daemon

import (
	"context"
	"path/filepath"

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
	// The record of who asked for which domain route, so one no stack
	// declares any more is pruned at the next doze trust; and the authority
	// for https, made on first use.
	domains := d.cfg.AppDomains()
	if err := names.RecordDomains(d.cfg.Home, filepath.Dir(d.cfg.Path()), domains); err != nil {
		d.logf("domains: could not record them: %v", err)
	}
	if len(domains) == 0 {
		return names.ServeIngress(ctx, d.zone, d.logf).Close, nil
	}
	ca, err := names.LoadOrCreateCA(names.CADir(d.cfg.Home))
	if err != nil {
		d.logf("domains: no certificate authority (%v); https is off", err)
		return names.ServeIngress(ctx, d.zone, d.logf).Close, nil
	}
	if d.usesDozeCA() {
		// What the stack's own processes need to verify https://<domain>.
		d.rt.SetExtraEnv(caEnv(ca.RootPath()))
	}
	for _, dom := range domains {
		if names.Wildcard(dom) {
			d.logf("front door: https://*.%s → the app", names.RouteDomain(dom))
		} else {
			d.logf("front door: https://%s → the app", dom)
		}
	}
	return names.ServeFrontDoor(ctx, d.zone, ca, d.logf).Close, nil
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
	for _, decl := range d.cfg.Decls() {
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
		// The app's own domains go to the same place, over https.
		for _, dom := range decl.Domains {
			routes[dom] = target
		}
	}
	return routes
}

// caEnv is the environment that points the usual clients at the doze
// authority: Node and Python never read the system store, Go and curl on
// macOS do but on Linux read SSL_CERT_FILE.
func caEnv(root string) []string {
	return []string{
		"DOZE_CA_FILE=" + root,
		"SSL_CERT_FILE=" + root,
		"NODE_EXTRA_CA_CERTS=" + root,
		"REQUESTS_CA_BUNDLE=" + root,
	}
}
