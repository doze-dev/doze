// The shared HTTP ingress: processes declaring `ingress = true` are served at
// http://<name>.<stack>.doze (port 80) — many services, one port, routed
// by Host header. Every daemon writes its routes into <home>/ingress.json;
// whichever daemon bound :80 first proxies for ALL stacks by re-reading that
// table, so the ingress survives any single stack going down (the next daemon
// to start reclaims the port). macOS allows unprivileged binds to :80; on
// Linux without CAP_NET_BIND_SERVICE the bind fails and doze says so.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/doze-dev/doze/internal/endpoints"
)

// IngressPort is the default HTTP front door for forwarded processes (the port a
// bare `ingress = true` / `forward = 80` uses).
const IngressPort = 80

// ingressRoute is one Host → local backend mapping.
type ingressRoute struct {
	Target string `json:"target"` // "127.0.0.1:8080"
	Port   int    `json:"port"`   // the public port this host is served on (80 by default)
	Stack  string `json:"stack"`
	PID    int    `json:"pid"` // the claiming daemon (stale routes are pruned by liveness)
}

func ingressPath(home string) string { return filepath.Join(home, "ingress.json") }

// setupIngress registers this stack's ingress routes and, if this daemon gets
// there first, starts the shared :80 proxy. The returned release func removes
// the stack's routes at shutdown.
func (d *Daemon) setupIngress(ctx context.Context, eps []endpoints.Endpoint, plan *bindPlan, awsRoutes map[string]awsRoute) (release func(), err error) {
	pid := os.Getpid()
	if len(awsRoutes) > 0 {
		publishAWSRoutes(d.cfg.Home, awsRoutes, pid)
		for host, rt := range awsRoutes {
			d.logf("aws: http://%s → %d %s resource(s)", host, len(rt.Resources), rt.Engine)
		}
	}
	routes := d.ingressRoutes(eps, plan)
	if len(routes) == 0 && len(awsRoutes) == 0 {
		if n := d.ingressDeclared(); n > 0 {
			d.logf("ingress: %d process(es) forward a port but derived no routes (check domains + ports)", n)
		}
		return func() {}, nil
	}
	if !d.cfg.Defaults.Domains {
		d.logf("ingress: forwarding needs defaults{domains=true} (hosts are domain names); skipping")
		return func() {}, nil
	}

	path := ingressPath(d.cfg.Home)
	release = func() {}
	if len(routes) > 0 {
		if err := mergeRoutes(path, routes, d.cfg.Stack(), pid); err != nil {
			return nil, fmt.Errorf("ingress: recording routes: %w", err)
		}
		release = func() { removeRoutes(path, d.cfg.Stack(), pid) }
	}

	// One shared handler served on every distinct forward port. Each listener
	// binds the wildcard address: macOS allows unprivileged low-port binds on
	// INADDR_ANY but (counterintuitively) NOT on 127.0.0.1 specifically. The
	// handler rejects every non-loopback client, so nothing is exposed off-box.
	// The :80 listener also fronts the AWS single endpoints.
	srv := &http.Server{Handler: newIngressHandler(path, d.cfg.Home), ReadHeaderTimeout: 10 * time.Second}
	bound := 0
	for _, port := range ingressPorts(routes, awsRoutes) {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			if isAddrInUse(err) {
				d.logf("ingress: :%d already served (another stack's daemon owns it)", port)
				continue
			}
			d.logf("ingress: cannot bind :%d (%v) — on Linux, grant CAP_NET_BIND_SERVICE or use a reverse proxy", port, err)
			continue
		}
		bound++
		go func() { _ = srv.Serve(ln) }()
	}
	if bound > 0 {
		go func() {
			<-ctx.Done()
			shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutCtx)
		}()
	}

	for host, r := range routes {
		d.logf("ingress: http://%s:%d → %s", host, ingressPortOr80(r.Port), r.Target)
	}
	return release, nil
}

// ingressPorts is the sorted set of wildcard ports to listen on: every route's
// public port, plus :80 whenever AWS single endpoints need fronting.
func ingressPorts(routes map[string]ingressRoute, awsRoutes map[string]awsRoute) []int {
	set := map[int]bool{}
	if len(awsRoutes) > 0 {
		set[IngressPort] = true
	}
	for _, r := range routes {
		set[ingressPortOr80(r.Port)] = true
	}
	ports := make([]int, 0, len(set))
	for p := range set {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}

func ingressPortOr80(p int) int {
	if p <= 0 {
		return IngressPort
	}
	return p
}

// ingressDeclared counts processes that opted in — for the skip diagnostics.
func (d *Daemon) ingressDeclared() int {
	n := 0
	for _, decl := range d.cfg.Instances {
		if decl.Type != "process" || !decl.Enabled {
			continue
		}
		if ing, ok := decl.Spec.(interface{ IngressEnabled() bool }); ok && ing.IngressEnabled() {
			n++
		}
	}
	return n
}

// ingressRoutes derives this stack's Host → target table.
func (d *Daemon) ingressRoutes(eps []endpoints.Endpoint, plan *bindPlan) map[string]ingressRoute {
	byName := map[string]endpoints.Endpoint{}
	for _, ep := range eps {
		byName[ep.Name] = ep
	}
	routes := map[string]ingressRoute{}
	for _, decl := range d.cfg.Instances {
		if decl.Type != "process" || !decl.Enabled {
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
		// The process self-binds its port on loopback; the ingress forwards there.
		target := ep.Address
		if b := plan.bind[decl.Name]; b != "" {
			target = b
		}
		routes[ep.Domain] = ingressRoute{Target: target, Port: fwd.ForwardPort(), Stack: d.cfg.Stack(), PID: os.Getpid()}
	}
	return routes
}

// mergeRoutes folds this stack's routes into the shared table, dropping any
// previous entries for the same stack and any entries whose daemon is dead.
func mergeRoutes(path string, routes map[string]ingressRoute, stack string, pid int) error {
	all := readRoutes(path)
	for host, r := range all {
		if r.Stack == stack || !pidAlive(r.PID) {
			delete(all, host)
		}
	}
	for host, r := range routes {
		all[host] = r
	}
	return writeRoutes(path, all)
}

func removeRoutes(path, stack string, pid int) {
	all := readRoutes(path)
	for host, r := range all {
		if r.Stack == stack && r.PID == pid {
			delete(all, host)
		}
	}
	_ = writeRoutes(path, all)
}

func readRoutes(path string) map[string]ingressRoute {
	all := map[string]ingressRoute{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &all)
	}
	return all
}

func writeRoutes(path string, all map[string]ingressRoute) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ingressHandler proxies by Host, re-reading the shared table when it changes
// so routes from every stack's daemon stay live without any coordination.
type ingressHandler struct {
	path string
	aws  *awsRouter

	mu      sync.Mutex
	loaded  time.Time
	routes  map[string]ingressRoute
	proxies map[string]*httputil.ReverseProxy
}

func newIngressHandler(path, home string) *ingressHandler {
	return &ingressHandler{path: path, aws: newAWSRouter(home), proxies: map[string]*httputil.ReverseProxy{}}
}

func (h *ingressHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Loopback only: the wildcard bind is a macOS low-port necessity, not an
	// invitation — local dev services must not be reachable from the network.
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err != nil || !net.ParseIP(ip).IsLoopback() {
		http.Error(w, "doze ingress serves loopback clients only", http.StatusForbidden)
		return
	}
	host := r.Host
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)

	// AWS single-endpoint hosts (s3./sqs./sns.<stack>.doze) route by
	// resource to the right backend.
	if rt, ok := h.aws.route(host); ok {
		h.aws.serve(w, r, rt)
		return
	}

	h.mu.Lock()
	if time.Since(h.loaded) > time.Second {
		h.routes = readRoutes(h.path)
		h.loaded = time.Now()
	}
	route, ok := h.routes[host]
	var proxy *httputil.ReverseProxy
	if ok {
		proxy = h.proxies[route.Target]
		if proxy == nil {
			proxy = httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: route.Target})
			h.proxies[route.Target] = proxy
		}
	}
	h.mu.Unlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "doze ingress: no service at %q — processes opt in with `ingress = true`\n", host)
		return
	}
	proxy.ServeHTTP(w, r)
}
