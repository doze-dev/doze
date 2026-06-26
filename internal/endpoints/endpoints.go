// Package endpoints derives each declared instance's client-facing address and
// connection string, and writes the .doze/endpoints.yaml manifest that
// `doze run`/`doze env` and external tooling consume.
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

	"github.com/nerdmenot/doze-sdk/engine"
	"github.com/nerdmenot/doze/internal/config"
)

// Endpoint is one instance's client-facing endpoint plus its connection string
// (shown by status/dash; doze does not inject it anywhere).
type Endpoint struct {
	Name    string `yaml:"name"`
	Engine  string `yaml:"engine"`
	Address string `yaml:"address"` // "host:port" or "unix:/path"
	EnvVar  string `yaml:"env_var,omitempty"`
	URL     string `yaml:"url,omitempty"`
}

// For computes the endpoints for every declared instance.
func For(cfg *config.Config) ([]Endpoint, error) {
	out := make([]Endpoint, 0, len(cfg.Instances))
	for _, decl := range cfg.Instances {
		addr, err := cfg.InstanceAddr(decl)
		if err != nil {
			return nil, err
		}
		ep := Endpoint{Name: decl.Name, Engine: decl.Type, Address: addr}
		if drv, ok := engine.Lookup(decl.Type); ok {
			eep := engineEndpoint(addr)
			inst := engine.Instance{Name: decl.Name, Type: decl.Type, Version: decl.Version, Endpoint: eep}
			ep.EnvVar, ep.URL = drv.ConnString(inst, eep)
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

func engineEndpoint(addr string) engine.Endpoint {
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		return engine.Endpoint{UnixSocket: path}
	}
	return engine.Endpoint{TCPAddr: addr}
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
