// Package endpoints derives each declared instance's client-facing address and
// connection string, and writes the .doze/endpoints.yaml manifest that the
// daemon, supervised `process` blocks, and external tooling consume.
//
// The address assignment lives here (not in daemon) so the CLI can compute the
// same endpoints the daemon listens on without importing the daemon.
package endpoints

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/config"
)

// Endpoint is one instance's client-facing endpoint plus its connection string
// (shown by status/dash; doze does not inject it anywhere).
type Endpoint struct {
	Name    string `yaml:"name"`
	Engine  string `yaml:"engine"`
	Address string `yaml:"address"` // "host:port" or "unix:/path" — the dialable truth
	// Domain is the instance's local DNS name (defaults{domains=true}):
	// <sanitized-name>.local, published over mDNS and resolving to 127.0.0.1.
	// When set, URL uses it in place of the loopback address.
	Domain string `yaml:"domain,omitempty"`
	EnvVar string `yaml:"env_var,omitempty"`
	URL    string `yaml:"url,omitempty"`
}

// For computes the endpoints for every declared instance.
func For(cfg *config.Config) ([]Endpoint, error) {
	out := make([]Endpoint, 0, len(cfg.Instances))
	for _, decl := range cfg.Instances {
		addr, err := cfg.InstanceAddr(decl)
		if err != nil {
			return nil, err
		}
		if addr == "" {
			continue // a portless process (worker) has no client-facing endpoint
		}
		ep := Endpoint{Name: decl.Name, Engine: decl.Type, Address: addr}
		if drv, ok := engine.Lookup(decl.Type); ok {
			eep := engineEndpoint(addr)
			inst := engine.Instance{Name: decl.Name, Type: decl.Type, Version: decl.Version, Endpoint: eep}
			ep.EnvVar, ep.URL = drv.ConnString(inst, eep)
		}
		// AWS built-ins share one port-less endpoint per type on the :80 ingress
		// (http://s3.<stack>.doze). Their high backend port in Address is an
		// internal detail — the daemon proxies to it — so they carry no
		// per-instance domain (only the shared host is published, by the AWS route
		// table), and their client-facing URL is the shared endpoint.
		if shared, ok := cfg.AWSEndpoint(decl.Type); ok {
			ep.URL = shared
		} else if cfg.Defaults.Domains && !strings.HasPrefix(addr, "unix:") {
			// Local DNS name: connection strings read as the service, not a loopback
			// address. Only TCP endpoints get one — a unix socket has no hostname.
			ep.Domain = cfg.DomainFor(decl.Name)
			if host, port, ok := splitHostPort(addr); ok && ep.URL != "" {
				ep.URL = strings.Replace(ep.URL, host+":"+port, ep.Domain+":"+port, 1)
			}
		}
		out = append(out, ep)
	}
	return out, nil
}

// ClientAddr returns the client-facing address assigned to a named instance.
func ClientAddr(cfg *config.Config, name string) (string, error) {
	if decl := cfg.Lookup(name); decl != nil {
		return cfg.InstanceAddr(decl)
	}
	return "", fmt.Errorf("instance %q is not declared", name)
}

// splitHostPort splits "host:port" without net's error ceremony.
func splitHostPort(addr string) (host, port string, ok bool) {
	i := strings.LastIndex(addr, ":")
	if i <= 0 || i == len(addr)-1 {
		return "", "", false
	}
	return addr[:i], addr[i+1:], true
}

func engineEndpoint(addr string) engine.Endpoint {
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		return engine.Endpoint{UnixSocket: path}
	}
	return engine.Endpoint{TCPAddr: addr}
}

// EnvPair is one exported connection variable: DATABASE_URL, AWS_ENDPOINT_URL_S3, …
type EnvPair struct {
	Service string `json:"service"`
	Engine  string `json:"engine"`
	Var     string `json:"var"`
	Value   string `json:"value"`
}

// EnvAssignments derives the env-var → connection-URL pairs for the declared
// services (all of them, or just the named ones) — the table `doze env`
// prints and the dash's :env palette command renders. Colliding variable
// names — two postgres instances both answering to DATABASE_URL — keep the
// first declaration bare and suffix later ones with the instance name. When
// any AWS-style endpoint is present, dummy credentials + region are appended
// (AWS SDKs refuse to sign without them; the local services ignore the values).
func EnvAssignments(cfg *config.Config, only []string) ([]EnvPair, error) {
	want := map[string]bool{}
	for _, n := range only {
		if cfg.Lookup(n) == nil {
			return nil, fmt.Errorf("instance %q is not declared", n)
		}
		want[n] = true
	}
	eps, err := For(cfg)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	var pairs []EnvPair
	aws := false
	for _, ep := range eps {
		if len(want) > 0 && !want[ep.Name] {
			continue
		}
		if ep.EnvVar == "" || ep.URL == "" {
			continue // no conventional client env var for this engine
		}
		// AWS built-ins share ONE port-less endpoint per service type
		// (http://s3.<stack>.doze), served on the :80 ingress — so a stock
		// SDK needs a single AWS_ENDPOINT_URL_S3/SQS/SNS for all buckets/queues/
		// topics, emitted once, not one per resource.
		if shared, ok := cfg.AWSEndpoint(ep.Engine); ok && strings.HasPrefix(ep.EnvVar, "AWS_ENDPOINT_URL") {
			aws = true
			if used[ep.EnvVar] {
				continue // one endpoint per type, already emitted
			}
			used[ep.EnvVar] = true
			pairs = append(pairs, EnvPair{Engine: ep.Engine, Var: ep.EnvVar, Value: shared})
			continue
		}
		v := ep.EnvVar
		if used[v] {
			v = v + "_" + envSuffix(ep.Name)
		}
		used[v] = true
		if strings.HasPrefix(ep.EnvVar, "AWS_ENDPOINT_URL") {
			aws = true
		}
		pairs = append(pairs, EnvPair{Service: ep.Name, Engine: ep.Engine, Var: v, Value: ep.URL})
	}
	if aws {
		for _, extra := range [][2]string{
			{"AWS_ACCESS_KEY_ID", "doze"},
			{"AWS_SECRET_ACCESS_KEY", "doze"},
			{"AWS_REGION", "us-east-1"},
		} {
			if !used[extra[0]] {
				used[extra[0]] = true
				pairs = append(pairs, EnvPair{Engine: "aws", Var: extra[0], Value: extra[1]})
			}
		}
	}
	return pairs, nil
}

// envSuffix turns an instance name into an env-var-safe suffix: "app-dev" → "APP_DEV".
func envSuffix(name string) string {
	up := strings.ToUpper(name)
	return strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, up)
}

// ManifestPath returns the .doze/endpoints.yaml path beside the config file.
func ManifestPath(cfg *config.Config) string {
	dir := "."
	if cfg.Path() != "" {
		dir = filepath.Dir(cfg.Path())
	}
	return filepath.Join(dir, ".doze", "endpoints.yaml")
}

// WriteManifest writes the endpoints manifest to path.
func WriteManifest(path string, eps []Endpoint) error {
	data, err := yaml.Marshal(struct {
		Endpoints []Endpoint `yaml:"endpoints"`
	}{eps})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
