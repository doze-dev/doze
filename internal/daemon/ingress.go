// The HTTP front door: an app that declares a `port` is also served at
// http://<name>.<stack>.doze, with no port, through the one :80 listener every
// doze program on the machine shares (doze-names). This file only says which
// names this stack wants fronted and where each goes; the listener, and the
// table it routes from, belong to the shared registry — so whichever of doze,
// doze-aws and doze-kafka started first serves all of them, from one file.
//
// The https door on :443 is the same listener's twin: bound whenever names
// are on, serving every declared domain on the machine with the certificate
// the person brought or one minted from the machine's authority as it is on
// disk now. So the holder need not be the stack that declared a domain, nor
// have started after the authority was made or rotated.
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
	d.ensureDomains(false)
	return names.ServeFrontDoor(ctx, d.zone, names.CADir(d.cfg.Home), d.logf).Close, nil
}

// ensureDomains makes the stack's declared domains good: it records them
// (so a route no stack declares any more is pruned at the next doze trust)
// and, when one is served from the doze authority, makes the authority and
// tells every process spawned from now on where it is. It runs at start
// and after a live change, so a domain added to a running stack gets the
// same treatment as one declared at the start; live says which.
func (d *Daemon) ensureDomains(live bool) {
	domains := d.cfg.AppDomains()
	if err := names.RecordDomains(d.cfg.Home, d.projectDir(), domains); err != nil {
		d.logf("domains: could not record them: %v", err)
	}
	if d.announced == nil {
		d.announced = map[string]bool{}
	}
	for _, dom := range domains {
		if d.announced[dom] {
			continue
		}
		d.announced[dom] = true
		if names.Wildcard(dom) {
			d.logf("front door: https://*.%s → the app", names.RouteDomain(dom))
		} else {
			d.logf("front door: https://%s → the app", dom)
		}
	}
	// The authority is made only when a domain is served from it; a stack
	// whose domains all bring their own files trusts nothing of doze's.
	if d.caEnvSet || !d.usesDozeCA() {
		return
	}
	ca, err := names.LoadOrCreateCA(names.CADir(d.cfg.Home))
	if err != nil {
		d.logf("domains: no certificate authority (%v); https is off for the minted domains", err)
		return
	}
	// What the stack's own processes need to verify https://<domain>: a
	// bundle of the machine's roots and this one, since the usual variables
	// replace a client's store rather than add to it.
	bundle, err := ca.Bundle()
	if err != nil {
		d.logf("domains: could not write the trust bundle: %v", err)
		return
	}
	if d.rt != nil {
		d.rt.SetExtraEnv(caEnv(ca.RootPath(), bundle))
	}
	d.caEnvSet = true
	if live {
		d.logf("domains: processes started from now on trust the authority; restart the ones already running to have them trust it too")
	}
}

// projectDir is the folder the config lives in, absolute and with symlinks
// resolved: the routes record keys projects by it, and the same folder
// must key the same way however it was reached.
func (d *Daemon) projectDir() string {
	dir := filepath.Dir(d.cfg.Path())
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	return dir
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
// authority. NODE_EXTRA_CA_CERTS adds to Node's store, so it gets the root;
// SSL_CERT_FILE and REQUESTS_CA_BUNDLE replace OpenSSL's and Python's, so
// they get the bundle that still holds the public roots.
func caEnv(root, bundle string) []string {
	return []string{
		"DOZE_CA_FILE=" + root,
		"NODE_EXTRA_CA_CERTS=" + root,
		"SSL_CERT_FILE=" + bundle,
		"REQUESTS_CA_BUNDLE=" + bundle,
	}
}
