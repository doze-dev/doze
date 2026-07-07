// The machine-wide stack registry: every running daemon claims its stack name
// in <home>/stacks.json so two projects can't both answer to
// *.shop.doze. Names are per-machine unique among LIVE stacks — a dead
// daemon's claim is stale and gets replaced.
package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// stackClaim is one stack's registration.
type stackClaim struct {
	Dir string `json:"dir"` // the project directory (config dir)
	PID int    `json:"pid"` // the claiming daemon
}

func stacksPath(home string) string { return filepath.Join(home, "stacks.json") }

// claimStack registers (name → dir, pid), failing when a DIFFERENT project's
// live daemon already owns the name. Same-dir reclaims and dead claims are
// replaced silently. Returns a release func for shutdown.
func claimStack(home, name, dir string, pid int) (func(), error) {
	path := stacksPath(home)
	claims := map[string]stackClaim{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &claims)
	}
	if cur, ok := claims[name]; ok && cur.Dir != dir && pidAlive(cur.PID) {
		return nil, fmt.Errorf("stack name %q is already in use by %s (daemon pid %d) — set a unique `name = \"…\"` in this project's doze.hcl", name, cur.Dir, cur.PID)
	}
	claims[name] = stackClaim{Dir: dir, PID: pid}
	if err := writeStacks(path, claims); err != nil {
		return nil, err
	}
	release := func() {
		claims := map[string]stackClaim{}
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, &claims)
		}
		if cur, ok := claims[name]; ok && cur.PID == pid {
			delete(claims, name)
			_ = writeStacks(path, claims)
		}
	}
	return release, nil
}

func writeStacks(path string, claims map[string]stackClaim) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(claims, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
