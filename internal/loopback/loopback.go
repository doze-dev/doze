// Package loopback hands each TCP service its own 127.0.0.x address so many
// services can share the SAME canonical port (every Postgres on 5432, every
// Redis on 6379) without colliding — the port is disambiguated by IP, and DNS
// maps each service name to its IP. Linux binds all of 127.0.0.0/8 for free;
// macOS needs the range aliased onto lo0 once (see `doze dns-setup`), so the
// daemon probes availability and falls back to unique ports on 127.0.0.1 when
// the range isn't set up.
package loopback

import (
	"fmt"
	"net"

	"github.com/doze-dev/doze/internal/netutil"
)

// Two ranges, and they are deliberately not the same range.
//
// ALIASING covers 127.0.0.2–127.0.0.65. The apex names of the shared zone live
// at .2–.9 (aws.doze, kafka.doze, …) and something has to alias them, or those
// names resolve to an address nothing can bind. `doze dns-setup` uses this.
//
// ALLOCATION starts at .10, because .2–.9 are spoken for by those apex names
// and handing one to a stack service would put two things on one address. .54
// is skipped too: it is where the resolver listens on Linux.
//
// The range stays SMALL on purpose: on macOS every address is aliased onto lo0
// and mDNSResponder registers each interface address, so a large alias set
// pressures that daemon (hundreds have been seen to peg it). A few dozen covers
// "many services share a canonical port" for real local dev. On Linux all of
// 127.0.0.0/8 binds for free and the cap is a formality. The allocator probes
// bindability and hands out only addresses that actually bind, so a stale
// larger or smaller alias set degrades gracefully.
const (
	aliasStart = 2
	aliasCount = 64 // 127.0.0.2 … 127.0.0.65

	hostStart = 10
	poolSize  = 56 // 127.0.0.10 … 127.0.0.65
	// resolverHost is excluded from the pool; see names.ResolverAddr.
	resolverHost = 54
)

// ipAt returns the nth address in the pool (n in [0, poolSize)).
func ipAt(n int) net.IP { return net.IPv4(127, 0, 0, byte(hostStart+n)) }

// bindable reports whether ip can be bound right now — true on Linux for all of
// 127/8, true on macOS only for addresses aliased onto lo0.
func bindable(ip net.IP) bool {
	l, _, err := netutil.ListenFree(ip.String())
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// Available reports whether the loopback range is usable — i.e. an address past
// .1 can actually be bound. On Linux this is always true; on macOS it is true
// only after `doze dns-setup` has aliased the range.
func Available() bool { return bindable(ipAt(0)) }

// SetupCommands returns the shell commands that alias the loopback pool onto
// lo0 for the current session (macOS). Linux needs nothing. Each line is safe
// to run under sudo.
func SetupCommands() []string {
	cmds := make([]string, 0, aliasCount)
	for n := 0; n < aliasCount; n++ {
		cmds = append(cmds, fmt.Sprintf("ifconfig lo0 alias 127.0.0.%d up", aliasStart+n))
	}
	return cmds
}

// LaunchdLabel / LaunchdPath / LaunchdPlist describe the boot-persistent
// aliasing daemon `doze dns-setup` installs so the range survives reboots.
const (
	LaunchdLabel = "dev.doze.loopback"
	LaunchdPath  = "/Library/LaunchDaemons/dev.doze.loopback.plist"
)

// LaunchdPlist renders the launchd job that re-aliases the pool at every boot.
func LaunchdPlist() string {
	script := fmt.Sprintf("for i in $(seq %d %d); do /sbin/ifconfig lo0 alias 127.0.0.$i up; done",
		aliasStart, aliasStart+aliasCount-1)
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + LaunchdLabel + `</string>
  <key>RunAtLoad</key><true/>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/sh</string>
    <string>-c</string>
    <string>` + script + `</string>
  </array>
</dict>
</plist>
`
}
