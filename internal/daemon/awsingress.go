// The AWS ingress: ONE host per stack — aws.<stack>.doze on the shared :80
// ingress — forwarded whole to the stack's aws instance. The instance is the
// entire local AWS (every service behind one gateway, routed by protocol, with
// the web console at /_console), so doze's job here is exactly one hop: host →
// backend. No path dispatch, no resource extraction, no synthesized calls.
package daemon

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/doze-dev/doze/internal/config"
)

// awsRoute is one stack's AWS forward: its host's single backend.
type awsRoute struct {
	Target string `json:"target"` // backend "ip:port"
	PID    int    `json:"pid"`
}

func awsRoutesPath(home string) string { return filepath.Join(home, "aws-ingress.json") }

// publishAWSRoutes records this stack's forward, dropping this pid's prior
// entries and any dead daemon's. The file is shared across stacks: whichever
// daemon holds :80 serves every stack's host from it.
func publishAWSRoutes(home string, routes map[string]awsRoute, pid int) {
	all := readAWSRoutes(home)
	for host, r := range all {
		if r.PID == pid || !pidAlive(r.PID) {
			delete(all, host)
		}
	}
	for host, r := range routes {
		all[host] = r
	}
	writeAWSRoutes(home, all)
}

func unpublishAWSRoutes(home string, pid int) {
	all := readAWSRoutes(home)
	for host, r := range all {
		if r.PID == pid {
			delete(all, host)
		}
	}
	writeAWSRoutes(home, all)
}

func readAWSRoutes(home string) map[string]awsRoute {
	out := map[string]awsRoute{}
	if data, err := os.ReadFile(awsRoutesPath(home)); err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

func writeAWSRoutes(home string, all map[string]awsRoute) {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return
	}
	if data, err := json.MarshalIndent(all, "", "  "); err == nil {
		tmp := awsRoutesPath(home) + ".tmp"
		if os.WriteFile(tmp, data, 0o644) == nil {
			_ = os.Rename(tmp, awsRoutesPath(home))
		}
	}
}

// awsRouter forwards AWS hosts to their instances, reloading the shared table
// lazily.
type awsRouter struct {
	home    string
	mu      sync.Mutex
	loaded  time.Time
	routes  map[string]awsRoute
	proxies map[string]*httputil.ReverseProxy
}

func newAWSRouter(home string) *awsRouter {
	return &awsRouter{home: home, proxies: map[string]*httputil.ReverseProxy{}}
}

// route returns the forward for host, if this is an AWS endpoint.
func (a *awsRouter) route(host string) (awsRoute, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if time.Since(a.loaded) > time.Second {
		a.routes = readAWSRoutes(a.home)
		a.loaded = time.Now()
	}
	r, ok := a.routes[host]
	return r, ok
}

func (a *awsRouter) proxyTo(target string) *httputil.ReverseProxy {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.proxies[target]
	if p == nil {
		p = httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: target})
		a.proxies[target] = p
	}
	return p
}

// serve is the whole router: one forward to the instance's backend.
func (a *awsRouter) serve(w http.ResponseWriter, r *http.Request, rt awsRoute) {
	if rt.Target == "" {
		http.Error(w, "doze: the aws instance has no backend", http.StatusBadGateway)
		return
	}
	a.proxyTo(rt.Target).ServeHTTP(w, r)
}

// buildAWSRoutes derives the stack's AWS forward (aws.<stack>.doze → the aws
// instance), points the host at the ingress (127.0.0.1) in the resolver, and
// returns the shared route table to publish.
func (d *Daemon) buildAWSRoutes(plan *bindPlan) map[string]awsRoute {
	host := "aws." + d.cfg.Stack() + "." + config.DomainSuffix
	for _, decl := range d.cfg.Instances {
		if !decl.Enabled || decl.Type != config.AWSUnifiedType {
			continue
		}
		target := plan.bind[decl.Name]
		if target == "" {
			continue
		}
		// The instance's directly-addressable thing is its console.
		if d.resources != nil {
			d.resources[decl.Name] = "http://" + host + "/" + config.AWSConsolePrefix
		}
		// The host resolves to the ingress (127.0.0.1:80, wildcard bind).
		plan.resolve[host] = net.IPv4(127, 0, 0, 1)
		return map[string]awsRoute{host: {Target: target, PID: os.Getpid()}}
	}
	return map[string]awsRoute{}
}
