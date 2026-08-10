// OS integration for the doze zone: whether this machine routes the suffix to
// the resolver, and how to make it.
//
// The resolver itself no longer lives here. It moved to doze-names, which
// doze, doze-aws and doze-kafka all serve, so the process holding the socket
// answers for every binary's names rather than only its own. Keeping a second
// copy here would have meant two servers competing for one port with two
// different registries behind them — whichever started first would have served
// the zone, and the other's names would have silently stopped resolving.
package daemon

import (
	"errors"
	"net"
	"os"
	"runtime"
	"strings"
	"syscall"

	names "github.com/doze-dev/doze-names"
)

// ResolverFile is the macOS resolver drop-in that routes *.doze queries to the
// zone.
const ResolverFile = "/etc/resolver/" + names.Suffix

// ResolverPort is the port the drop-in must name — taken from doze-names
// rather than restated, so the file this writes and the socket that binds
// cannot drift apart.
func ResolverPort() string {
	_, port, err := net.SplitHostPort(names.ResolverAddr())
	if err != nil {
		return ""
	}
	return port
}

// ResolverSetupHint is the one-time command that installs the drop-in.
var ResolverSetupHint = `sudo sh -c 'mkdir -p /etc/resolver && printf "nameserver 127.0.0.1\nport ` +
	ResolverPort() + `\n" > ` + ResolverFile + `'`

// ResolverConfigured reports whether the OS routes doze to the zone (macOS:
// the drop-in exists and names our port).
func ResolverConfigured() bool {
	if runtime.GOOS != "darwin" {
		return false // linux setup is distro-specific; doctor explains
	}
	data, err := os.ReadFile(ResolverFile)
	if err != nil {
		return false
	}
	s := string(data)
	return strings.Contains(s, "127.0.0.1") && strings.Contains(s, ResolverPort())
}

func isAddrInUse(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) {
		return errors.Is(op.Err, syscall.EADDRINUSE)
	}
	return errors.Is(err, syscall.EADDRINUSE)
}
