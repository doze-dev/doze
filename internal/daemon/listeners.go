package daemon

import (
	"fmt"
	"net"

	"github.com/doze-dev/doze-sdk/engine"

	"github.com/doze-dev/doze/internal/netplan"
)

// A module's listeners (engine.Networked): every port of an instance is on the
// instance's own address, held by doze, so a connection to any of them wakes
// it. The main listener is the instance's proxy listener, as for every module;
// each other one is forwarded to the socket the module listens on for it. An
// HTTP listener's route names resolve to the instance's address and the front
// door sends them to that listener. See docs/design/next/module-names.md.

// listenerBind is one listener of an instance: where doze listens, the socket
// it forwards to (empty for main: the proxy dials the instance's backend), and
// the names routed to it.
type listenerBind struct {
	Name    string
	Public  string
	Backend string
	Hosts   []string
}

// listenersOf places an instance's listeners on mainBind's host, main first.
func (d *Daemon) listenersOf(name, mainBind string) ([]listenerBind, error) {
	decl := d.cfg.Lookup(name)
	if decl == nil {
		return nil, fmt.Errorf("no instance named %q", name)
	}
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return nil, fmt.Errorf("no engine %q", decl.Type)
	}
	g, err := d.cfg.Grant(decl, drv, "")
	if err != nil {
		return nil, err
	}
	host, _, err := net.SplitHostPort(mainBind)
	if err != nil {
		return nil, fmt.Errorf("%q listens on %q, which is not host:port", name, mainBind)
	}
	if len(g.Listeners) == 0 {
		return []listenerBind{{Name: engine.MainListener, Public: mainBind}}, nil
	}
	var out []listenerBind
	for _, ln := range netplan.ListenerNames(g) {
		gl := g.Listeners[ln]
		b := listenerBind{Name: ln, Public: mainBind, Hosts: gl.Hosts}
		if ln != engine.MainListener {
			_, port, _ := net.SplitHostPort(gl.Addr)
			b.Public, b.Backend = net.JoinHostPort(host, port), gl.Backend
		}
		out = append(out, b)
	}
	return out, nil
}

// startListeners serves an instance's listeners other than main and records
// where its route names go. The caller holds d.mu (or runs during Run) and has
// started the main listener on mainBind; serve runs a forward until the
// instance is stopped.
func (d *Daemon) startListeners(name, engineType, mainBind string, ip net.IP, serve func(l net.Listener, backend string)) error {
	binds, err := d.listenersOf(name, mainBind)
	if err != nil {
		return err
	}
	if ip == nil {
		if host, _, err := net.SplitHostPort(mainBind); err == nil {
			ip = net.ParseIP(host)
		}
	}
	routes := map[string]string{}
	for _, b := range binds {
		if b.Name != engine.MainListener {
			l, err := d.listen(b.Public)
			if err != nil {
				return fmt.Errorf("listening for %q's %s listener on %s: %w", name, b.Name, b.Public, err)
			}
			serve(l, b.Backend)
			d.logf("%s/%s %s on %s", engineType, name, b.Name, b.Public)
		}
		for _, h := range b.Hosts {
			routes[h] = b.Public
			if ip != nil && d.resolveMap != nil {
				d.resolveMap[h] = ip
			}
		}
	}
	if d.instRoutes == nil {
		d.instRoutes = map[string]map[string]string{}
	}
	d.instRoutes[name] = routes
	return nil
}

// moduleRoutes is every instance's route names, each to its listener's public
// address. The caller holds d.mu (or runs during Run).
func (d *Daemon) moduleRoutes() map[string]string {
	out := map[string]string{}
	for _, routes := range d.instRoutes {
		for h, target := range routes {
			out[h] = target
		}
	}
	return out
}

// forgetListeners drops an instance's route names, whose listeners stop with
// it. The caller holds d.mu and republishes the names after.
func (d *Daemon) forgetListeners(name string) {
	for h := range d.instRoutes[name] {
		delete(d.resolveMap, h)
	}
	delete(d.instRoutes, name)
}
