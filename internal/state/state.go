// Package state persists, per project, the set of structural objects doze has
// applied for each instance (roles, databases, buckets, queues, …). It is the
// record `doze plan`/`apply`/`destroy` diff against: apply prunes objects that
// were applied before but are no longer declared, and destroy prunes them all.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nerdmenot/doze/internal/engine"
)

// stateVersion is bumped if the on-disk shape changes incompatibly.
const stateVersion = 1

// State is the applied-object record for a project.
type State struct {
	Version int `json:"version"`
	// Instances maps an instance name to the objects doze has applied for it.
	Instances map[string][]engine.Object `json:"instances"`
	// Outputs records the last-applied output values (name → rendered value).
	Outputs map[string]string `json:"outputs,omitempty"`
}

// New returns an empty state.
func New() *State {
	return &State{Version: stateVersion, Instances: map[string][]engine.Object{}, Outputs: map[string]string{}}
}

// Objects returns the applied objects for an instance (nil if none).
func (s *State) Objects(instance string) []engine.Object { return s.Instances[instance] }

// Set records the applied objects for an instance (replacing any prior set);
// passing none clears the entry.
func (s *State) Set(instance string, objs []engine.Object) {
	if len(objs) == 0 {
		delete(s.Instances, instance)
		return
	}
	s.Instances[instance] = objs
}

// Path returns the state file path for a config: .doze/state.json beside it.
func Path(configPath string) string {
	dir := "."
	if configPath != "" {
		if fi, err := os.Stat(configPath); err == nil && fi.IsDir() {
			dir = configPath
		} else {
			dir = filepath.Dir(configPath)
		}
	}
	return filepath.Join(dir, ".doze", "state.json")
}

// Load reads the state at path, returning an empty state if it does not exist.
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if s.Instances == nil {
		s.Instances = map[string][]engine.Object{}
	}
	if s.Outputs == nil {
		s.Outputs = map[string]string{}
	}
	return &s, nil
}

// Save writes the state to path atomically (temp file + rename).
func (s *State) Save(path string) error {
	s.Version = stateVersion
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Lock takes an exclusive advisory lock for the state at path, so two applies
// can't race. It returns an unlock func. Stale locks (older than ttl) are broken.
func Lock(path string) (unlock func(), err error) {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	const ttl = 2 * time.Minute
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		// Break a stale lock (a crashed apply) rather than wedge forever.
		if fi, statErr := os.Stat(lockPath); statErr == nil && timeSince(fi.ModTime()) > ttl {
			_ = os.Remove(lockPath)
			continue
		}
		return nil, fmt.Errorf("another doze apply/destroy is in progress (remove %s if stale)", lockPath)
	}
	return nil, fmt.Errorf("could not acquire state lock %s", lockPath)
}

// timeSince is a tiny indirection so tests don't need a real clock.
var timeSince = time.Since
