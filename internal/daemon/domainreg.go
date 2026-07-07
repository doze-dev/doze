// The machine-wide domain registry: every daemon publishes its
// <service>.<stack>.doze → IP mappings to <home>/domains.json, and every
// daemon's resolver answers from the UNION. This matters because only ONE daemon
// binds the unicast resolver on 127.0.0.1:5323 (the rest get address-in-use), so
// that single owner must be able to resolve names for every stack on the machine
// — which it does by answering from the shared union rather than just its own map.
package daemon

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
)

func domainsPath(home string) string { return filepath.Join(home, "domains.json") }

type domainEntry struct {
	IP  string `json:"ip"`
	PID int    `json:"pid"`
}

var domainsMu sync.Mutex

// publishDomains records this daemon's name→IP mappings in the shared file,
// dropping any stale entries (this pid's previous set, plus dead daemons').
func publishDomains(home string, names map[string]net.IP, pid int) {
	domainsMu.Lock()
	defer domainsMu.Unlock()
	all := readDomainEntries(home)
	for name, e := range all {
		if e.PID == pid || !pidAlive(e.PID) {
			delete(all, name)
		}
	}
	for name, ip := range names {
		all[name] = domainEntry{IP: ip.String(), PID: pid}
	}
	writeDomainEntries(home, all)
}

// unpublishDomains removes this daemon's entries at shutdown.
func unpublishDomains(home string, pid int) {
	domainsMu.Lock()
	defer domainsMu.Unlock()
	all := readDomainEntries(home)
	for name, e := range all {
		if e.PID == pid {
			delete(all, name)
		}
	}
	writeDomainEntries(home, all)
}

// sharedResolve looks a name up in the shared file (for names this daemon
// doesn't own itself). Returns nil when unknown.
func sharedResolve(home, name string) net.IP {
	domainsMu.Lock()
	defer domainsMu.Unlock()
	if e, ok := readDomainEntries(home)[name]; ok {
		return net.ParseIP(e.IP)
	}
	return nil
}

func readDomainEntries(home string) map[string]domainEntry {
	out := map[string]domainEntry{}
	if data, err := os.ReadFile(domainsPath(home)); err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

func writeDomainEntries(home string, all map[string]domainEntry) {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return
	}
	if data, err := json.MarshalIndent(all, "", "  "); err == nil {
		tmp := domainsPath(home) + ".tmp"
		if os.WriteFile(tmp, data, 0o644) == nil {
			_ = os.Rename(tmp, domainsPath(home))
		}
	}
}
