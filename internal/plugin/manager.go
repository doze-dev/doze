package plugin

import (
	"fmt"
	"sync"

	"github.com/nerdmenot/doze/internal/engine"
)

// Resolver locates the plugin binary for an engine type, returning ok=false when
// that engine is compiled in (not a plugin). For v1 a path comes from a local
// override or the doze-modules cache; Phase 5 adds fetch+pin from the monorepo.
type Resolver func(engineType string) (path string, env []string, ok bool)

// Manager owns the launched engine-plugin processes for a daemon: it resolves an
// engine type to a plugin binary, launches it on first use, keeps it warm (config
// eval and every boot reuse the one process), and reaps them all on Close. A
// plugin that has exited is relaunched on the next request.
type Manager struct {
	resolve Resolver
	mu      sync.Mutex
	hosts   map[string]*Host
}

// NewManager builds a Manager backed by resolve.
func NewManager(resolve Resolver) *Manager {
	return &Manager{resolve: resolve, hosts: map[string]*Host{}}
}

// Driver returns the warm plugin driver for engineType, launching it if needed.
// found is false when the engine is not a plugin (the caller falls back to the
// in-tree registry).
func (m *Manager) Driver(engineType string) (drv engine.Driver, found bool, err error) {
	path, env, ok := m.resolve(engineType)
	if !ok {
		return nil, false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if h := m.hosts[engineType]; h != nil {
		if h.Alive() {
			return h.Driver(), true, nil
		}
		h.Close() // exited/crashed — drop and relaunch
		delete(m.hosts, engineType)
	}
	h, err := Launch(path, env)
	if err != nil {
		return nil, true, fmt.Errorf("launching %s plugin: %w", engineType, err)
	}
	m.hosts[engineType] = h
	return h.Driver(), true, nil
}

// Close reaps every launched plugin.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, h := range m.hosts {
		h.Close()
		delete(m.hosts, name)
	}
}
