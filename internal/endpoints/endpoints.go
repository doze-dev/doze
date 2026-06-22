// Package endpoints derives each declared instance's client-facing address and
// connection string, and writes the .doze/endpoints.yaml manifest that
// `doze run`/`doze env` and external tooling consume.
//
// The address assignment lives here (not in daemon) so the CLI can compute the
// same endpoints the daemon listens on without importing the daemon.
package endpoints

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/engine"
)

// Endpoint is one instance's client-facing endpoint plus its injectable
// connection string.
type Endpoint struct {
	Name    string `yaml:"name"`
	Engine  string `yaml:"engine"`
	Address string `yaml:"address"` // "host:port" or "unix:/path"
	EnvVar  string `yaml:"env_var,omitempty"`
	URL     string `yaml:"url,omitempty"`
	// Extra is engine-contributed environment beyond the ConnString pair (an
	// engine.EnvProvider), e.g. the AWS services' endpoint URL + dummy creds.
	Extra map[string]string `yaml:"extra,omitempty"`
}

// For computes the endpoints for every declared instance.
func For(cfg *config.Config) ([]Endpoint, error) {
	out := make([]Endpoint, 0, len(cfg.Instances))
	for i, decl := range cfg.Instances {
		addr, err := clientAddr(cfg.Listen, decl, i)
		if err != nil {
			return nil, err
		}
		ep := Endpoint{Name: decl.Name, Engine: decl.Type, Address: addr}
		if drv, ok := engine.Lookup(decl.Type); ok {
			eep := engineEndpoint(addr)
			inst := engine.Instance{Name: decl.Name, Type: decl.Type, Version: decl.Version, Endpoint: eep}
			ep.EnvVar, ep.URL = drv.ConnString(inst, eep)
			if envp, ok := drv.(engine.EnvProvider); ok {
				ep.Extra = envp.Env(inst, eep)
			}
		}
		out = append(out, ep)
	}
	return out, nil
}

// ClientAddr returns the client-facing address assigned to a named instance.
func ClientAddr(cfg *config.Config, name string) (string, error) {
	for i, decl := range cfg.Instances {
		if decl.Name == name {
			return clientAddr(cfg.Listen, decl, i)
		}
	}
	return "", fmt.Errorf("instance %q is not declared", name)
}

// clientAddr derives an instance's client-facing address: a per-instance
// `listen` override wins; otherwise, for a TCP base, each instance gets
// base_port+index; for a unix base, a per-instance socket beside it.
func clientAddr(base string, decl *config.InstanceDecl, index int) (string, error) {
	if decl.Listen != "" {
		return decl.Listen, nil
	}
	if path, ok := strings.CutPrefix(base, "unix:"); ok {
		return "unix:" + filepath.Join(filepath.Dir(path), decl.Name+".sock"), nil
	}
	host, portStr, err := net.SplitHostPort(base)
	if err != nil {
		return "", fmt.Errorf("invalid listen address %q: %w", base, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", fmt.Errorf("invalid listen port %q: %w", portStr, err)
	}
	return net.JoinHostPort(host, strconv.Itoa(port+index)), nil
}

func engineEndpoint(addr string) engine.Endpoint {
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		return engine.Endpoint{UnixSocket: path}
	}
	return engine.Endpoint{TCPAddr: addr}
}

// EnvVars builds the environment doze injects: a unique DOZE_<NAME>_URL for
// every instance, plus the conventional engine variable (DATABASE_URL, …) when
// exactly one instance claims it (so it stays unambiguous).
func EnvVars(eps []Endpoint) map[string]string {
	convCount := map[string]int{}
	for _, ep := range eps {
		if ep.EnvVar != "" && ep.URL != "" {
			convCount[ep.EnvVar]++
		}
	}
	m := map[string]string{}
	for _, ep := range eps {
		// Engine-contributed extras (e.g. AWS creds + region) apply even when the
		// instance has no single ConnString URL.
		for k, v := range ep.Extra {
			m[k] = v
		}
		if ep.URL == "" {
			continue
		}
		m["DOZE_"+sanitizeEnv(ep.Name)+"_URL"] = ep.URL
		if ep.EnvVar != "" && convCount[ep.EnvVar] == 1 {
			m[ep.EnvVar] = ep.URL
		}
	}
	return m
}

func sanitizeEnv(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
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
