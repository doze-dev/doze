// Package netplan turns what a module says it serves (engine.Networked) and
// what the stack's configuration says (names on or off, the declared port,
// `front`) into what doze sets up for an instance: each listener's address,
// its backend socket, and the names its HTTP routes answer for
// (engine.Grant).
//
// It is pure, so the CLI (endpoints, `doze env`, references) and the daemon
// compute the same answer without starting anything. See
// docs/design/next/module-names.md.
package netplan

import (
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/doze-dev/doze-sdk/engine"
)

// Network is an instance's network: the module's, or engine.DefaultNetwork when
// it declares none, with the main listener on the declared port. It is an error
// when the module's network is not one doze will take, or when another of its
// listeners uses the declared port.
func Network(drv engine.Driver, inst engine.Instance, port int) (engine.Network, error) {
	n := declared(drv, inst)
	if len(n.Listeners) == 0 {
		n = engine.DefaultNetwork()
	}
	if err := engine.ValidateNetwork(n); err != nil {
		return engine.Network{}, fmt.Errorf("the %s module's listeners for %q: %w", inst.Type, inst.Name, err)
	}
	out := engine.Network{Listeners: make([]engine.Listener, len(n.Listeners))}
	for i, l := range n.Listeners {
		if l.Name == engine.MainListener {
			l.Port = port
		} else if l.Port == port {
			return engine.Network{}, fmt.Errorf("the %s module's listener %q for %q is on port %d, which is the instance's own port: give the instance another `port`", inst.Type, l.Name, inst.Name, port)
		}
		l.Routes = append([]string(nil), l.Routes...)
		out.Listeners[i] = l
	}
	return out, nil
}

// Declared says whether the module declares a network of its own.
func Declared(drv engine.Driver, inst engine.Instance) bool {
	return len(declared(drv, inst).Listeners) > 0
}

func declared(drv engine.Driver, inst engine.Instance) engine.Network {
	if nw, ok := drv.(engine.Networked); ok {
		return nw.Network(inst)
	}
	return engine.Network{}
}

// Place is where an instance is: what the stack's configuration says about it.
type Place struct {
	// Host is the instance's name (events.shop.doze), or "" when names are off.
	Host string
	// AddrHost is the host a listener is dialed at without a name: 127.0.0.1.
	AddrHost string
	// Front says whether its routes are served: names are on and the instance
	// has not set `front = false`.
	Front bool
	// SocketDir is the instance's socket directory, where a listener other than
	// main has its backend; MainBackend is main's (Driver.BackendSocket).
	SocketDir, MainBackend string
}

// Grant is what an instance's listeners get, in place p.
func Grant(n engine.Network, p Place) engine.Grant {
	g := engine.Grant{Host: p.Host, Listeners: map[string]engine.GrantedListener{}}
	host := p.Host
	if host == "" {
		host = p.AddrHost
	}
	for _, l := range n.Listeners {
		gl := engine.GrantedListener{Addr: net.JoinHostPort(host, strconv.Itoa(l.Port)), Backend: Backend(l.Name, p.SocketDir, p.MainBackend)}
		if p.Host != "" && p.Front {
			for _, r := range l.Routes {
				if h, err := engine.RouteHost(r, p.Host); err == nil {
					gl.Hosts = append(gl.Hosts, h)
				}
			}
		}
		if l.Protocol == engine.ProtocolHTTP {
			gl.URL = "http://" + gl.Addr
			if len(gl.Hosts) > 0 {
				gl.URL = "http://" + gl.Hosts[0]
			}
		}
		g.Listeners[l.Name] = gl
	}
	return g
}

// Backend is the socket a listener's backend listens on: main's is the
// driver's, every other one is <listener>.sock beside it.
func Backend(listener, socketDir, mainBackend string) string {
	if listener == engine.MainListener {
		return mainBackend
	}
	return filepath.Join(socketDir, listener+".sock")
}

// Route is one name the front door sends to one of an instance's listeners.
type Route struct {
	Host     string
	Listener string
}

// Routes are the names a grant's listeners answer for, in listener order.
func Routes(g engine.Grant) []Route {
	var out []Route
	for _, name := range ListenerNames(g) {
		for _, h := range g.Listeners[name].Hosts {
			out = append(out, Route{Host: h, Listener: name})
		}
	}
	return out
}

// ListenerNames is the grant's listeners with main first, then by name, so
// what is derived from a grant comes out the same every time.
func ListenerNames(g engine.Grant) []string {
	var rest []string
	for name := range g.Listeners {
		if name != engine.MainListener {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	if _, ok := g.Listeners[engine.MainListener]; ok {
		return append([]string{engine.MainListener}, rest...)
	}
	return rest
}
