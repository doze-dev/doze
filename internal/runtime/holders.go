package runtime

import (
	"sort"

	"github.com/doze-dev/doze/internal/registry"
)

// The kinds of thing that keep a running instance out of the idle reaper.
const (
	HolderDependant  = "dependant"  // a running instance that references it
	HolderPin        = "pin"        // the user pinned it awake
	HolderConnection = "connection" // clients connected through the proxy
)

// Holder is one reason an instance is awake.
type Holder struct {
	Kind string
	// Name is the dependant's name. Empty for a pin and for connections.
	Name string
	// Count is the number of open client connections. Zero for the other kinds.
	Count int
}

// Holders reports why name is awake: the running instances that depend on it,
// a pin, and the client connections open to it. It is empty for an instance
// that is not running.
//
// A dependant holds a dependency by taking a connection slot on it (Acquire),
// so the registry's connection count includes dependants. Holders separates
// the two: what it calls connections are clients only.
func (r *Runtime) Holders(name string) []Holder {
	inst, ok := r.reg.Get(name)
	if !ok || (inst.State != registry.Active && inst.State != registry.Idle) {
		return nil
	}
	r.mu.Lock()
	var dependants []string
	for dependant, held := range r.deps {
		for _, dep := range held {
			if dep == name {
				dependants = append(dependants, dependant)
				break
			}
		}
	}
	r.mu.Unlock()
	sort.Strings(dependants)

	var out []Holder
	for _, d := range dependants {
		out = append(out, Holder{Kind: HolderDependant, Name: d})
	}
	if inst.KeepAwake {
		out = append(out, Holder{Kind: HolderPin})
	}
	if clients := inst.Conns - len(dependants); clients > 0 {
		out = append(out, Holder{Kind: HolderConnection, Count: clients})
	}
	return out
}
