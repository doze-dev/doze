// Package loopback hands each TCP service its own 127.0.0.x address so many
// services can share the SAME canonical port (every Postgres on 5432, every
// Redis on 6379) without colliding — the port is disambiguated by IP, and DNS
// maps each service name to its IP. Linux binds all of 127.0.0.0/8 for free;
// macOS needs the range aliased onto lo0 once (see `doze dns-setup`), so the
// daemon probes availability and falls back to unique ports on 127.0.0.1 when
// the range isn't set up.
package loopback

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/doze-dev/doze/internal/netutil"
)

// The pool is a small contiguous range in the first /24 — 127.0.0.2 … .(1+size).
// Deliberately SMALL: on macOS every address has to be aliased onto lo0 (see
// `doze dns-setup`) and macOS's mDNSResponder registers each interface address,
// so a large alias set pressures that daemon (hundreds of aliases have been seen
// to peg it). A few dozen covers "many services share a canonical port" for real
// local dev — bump poolSize (and re-run dns-setup) only if you truly run more
// same-port services than this at once. On Linux all of 127.0.0.0/8 binds for
// free; the cap is only a formality there. The allocator probes bindability and
// hands out only addresses that actually bind, so a stale larger/smaller alias
// set degrades gracefully.
const (
	hostStart = 2
	poolSize  = 64 // 127.0.0.2 … 127.0.0.65
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

// alloc is one instance's assigned address plus the daemon that holds it (for
// liveness reaping — a dead stack's IPs return to the pool).
type alloc struct {
	IP  string `json:"ip"`
	PID int    `json:"pid"`
}

// Allocator assigns stable per-instance loopback IPs, backed by a shared file
// so allocations are consistent across a machine's stacks. Assignments persist
// across restarts (same instance → same IP) and are reclaimed when the owning
// daemon dies.
type Allocator struct {
	path string
	pid  int

	mu   sync.Mutex
	held map[string]net.IP // key -> ip, this process's assignments
}

// NewAllocator backs allocations with <home>/loopback.json.
func NewAllocator(home string, pid int) *Allocator {
	return &Allocator{
		path: filepath.Join(home, "loopback.json"),
		pid:  pid,
		held: map[string]net.IP{},
	}
}

// For returns the loopback IP for (stack, instance), allocating one on first
// use. The same key always maps to the same IP while this daemon lives.
func (a *Allocator) For(stack, instance string) (net.IP, error) {
	key := stack + "/" + instance
	a.mu.Lock()
	defer a.mu.Unlock()
	if ip, ok := a.held[key]; ok {
		return ip, nil
	}

	all := readAllocs(a.path)
	// Reap allocations owned by dead daemons so their IPs return to the pool.
	inUse := map[string]bool{}
	for k, al := range all {
		if k != key && !pidAlive(al.PID) {
			delete(all, k)
			continue
		}
		inUse[al.IP] = true
	}
	// Reuse this key's prior IP if it's still recorded and still bindable, else
	// take the lowest free address that actually binds — skipping any the OS won't
	// assign (on macOS, addresses `doze dns-setup` hasn't aliased yet).
	var chosen net.IP
	if al, ok := all[key]; ok {
		if ip := net.ParseIP(al.IP); ip != nil && bindable(ip) {
			chosen = ip
		}
	}
	if chosen == nil {
		for n := 0; n < poolSize; n++ {
			ip := ipAt(n)
			if inUse[ip.String()] || !bindable(ip) {
				continue
			}
			chosen = ip
			break
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("loopback pool exhausted — every bindable address is taken; run `doze dns-setup` to alias more (or an unusual number of stacks are running)")
	}
	all[key] = alloc{IP: chosen.String(), PID: a.pid}
	if err := writeAllocs(a.path, all); err != nil {
		return nil, err
	}
	a.held[key] = chosen
	return chosen, nil
}

// Release drops this daemon's allocations from the shared file (called at
// shutdown so the IPs free immediately rather than waiting on pid-liveness).
func (a *Allocator) Release() {
	a.mu.Lock()
	defer a.mu.Unlock()
	all := readAllocs(a.path)
	for k, al := range all {
		if al.PID == a.pid {
			delete(all, k)
		}
	}
	_ = writeAllocs(a.path, all)
	a.held = map[string]net.IP{}
}

func readAllocs(path string) map[string]alloc {
	out := map[string]alloc{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

func writeAllocs(path string, all map[string]alloc) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// SetupCommands returns the shell commands that alias the loopback pool onto
// lo0 for the current session (macOS). Linux needs nothing. Each line is safe
// to run under sudo.
func SetupCommands() []string {
	cmds := make([]string, 0, poolSize)
	for n := 0; n < poolSize; n++ {
		cmds = append(cmds, fmt.Sprintf("ifconfig lo0 alias %s up", ipAt(n)))
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
		hostStart, hostStart+poolSize-1)
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
