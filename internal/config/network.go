package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/doze-dev/doze-sdk/engine"

	"github.com/doze-dev/doze/internal/netplan"
)

// The ports an instance serves and the names it answers for: the module's
// listeners (engine.Networked, or one tcp listener on the declared port) placed
// by this configuration. See internal/netplan and docs/design/next/module-names.md.

// Network is decl's network, with its main listener on the declared port.
func (c *Config) Network(decl *InstanceDecl, drv engine.Driver) (engine.Network, error) {
	return netplan.Network(drv, engine.Instance{Name: decl.Name, Type: decl.Type, Version: decl.Version, Spec: decl.Spec}, decl.Port)
}

// Place is where decl is: its name when names are on, the host its listeners
// are on without one, whether its routes are served, and its socket directory.
func (c *Config) Place(decl *InstanceDecl, mainBackend string) netplan.Place {
	p := netplan.Place{AddrHost: "127.0.0.1", SocketDir: c.SocketDir(decl.Name), MainBackend: mainBackend}
	if host, _, err := net.SplitHostPort(decl.Listen); err == nil && host != "" {
		p.AddrHost = host
	}
	if c.Defaults.Domains {
		p.Host = c.DomainFor(decl.Name)
		p.Front = !decl.NoRoutes
	}
	return p
}

// Grant is what decl's listeners get in this configuration. An instance with no
// port (a worker) and no listeners of its own gets none.
func (c *Config) Grant(decl *InstanceDecl, drv engine.Driver, mainBackend string) (engine.Grant, error) {
	inst := engine.Instance{Name: decl.Name, Type: decl.Type, Spec: decl.Spec}
	if decl.Port == 0 && !netplan.Declared(drv, inst) {
		return engine.Grant{}, nil
	}
	n, err := c.Network(decl, drv)
	if err != nil {
		return engine.Grant{}, err
	}
	return netplan.Grant(n, c.Place(decl, mainBackend)), nil
}

// validateNetworks checks every enabled module's listeners as the daemon will
// place them: a network doze takes, and route names that are no other
// instance's name.
func (c *Config) validateNetworks() error {
	var errs []error
	names := map[string]string{} // an instance's name -> the instance
	if c.Defaults.Domains {
		for _, d := range c.Instances {
			if d.Enabled {
				names[c.DomainFor(d.Name)] = d.Name
			}
		}
	}
	for _, d := range c.Instances {
		if !d.Enabled {
			continue
		}
		drv, ok := engine.Lookup(d.Type)
		if !ok {
			continue
		}
		g, err := c.Grant(d, drv, "")
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, r := range netplan.Routes(g) {
			if other, taken := names[r.Host]; taken && other != d.Name {
				errs = append(errs, fmt.Errorf("%q asks for the name %s for its %q listener, which is the name of %q: rename one of them", d.Name, r.Host, r.Listener, other))
			}
		}
	}
	return errors.Join(errs...)
}

// extraListenerAddrs are the addresses of decl's listeners other than main, on
// 127.0.0.1 without names, each with what it is, for the port check.
func (c *Config) extraListenerAddrs(decl *InstanceDecl) map[string]string {
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return nil
	}
	n, err := c.Network(decl, drv)
	if err != nil {
		return nil
	}
	host := c.Place(decl, "").AddrHost
	out := map[string]string{}
	for _, l := range n.Listeners {
		if l.Name != engine.MainListener {
			out[net.JoinHostPort(host, strconv.Itoa(l.Port))] = fmt.Sprintf("%q (its %s listener)", decl.Name, l.Name)
		}
	}
	return out
}
