package state

import (
	"github.com/nerdmenot/doze/internal/config"
	"github.com/nerdmenot/doze/internal/engine"
)

// ChangeKind is the action a plan entry represents.
type ChangeKind int

const (
	Create ChangeKind = iota
	Update
	Delete
)

func (k ChangeKind) Symbol() string {
	switch k {
	case Create:
		return "+"
	case Update:
		return "~"
	default:
		return "-"
	}
}

// Change is one object create/update/delete within an instance.
type Change struct {
	Kind   ChangeKind
	Object engine.Object
}

// InstancePlan is the set of changes for one instance.
type InstancePlan struct {
	Name    string
	Engine  string
	Changes []Change
}

// Plan is the full set of structural changes apply (or destroy) would make.
type Plan struct {
	Instances []InstancePlan
}

// Empty reports whether the plan makes no changes.
func (p Plan) Empty() bool {
	for _, ip := range p.Instances {
		if len(ip.Changes) > 0 {
			return false
		}
	}
	return true
}

// Counts returns the number of objects to add, change, and destroy.
func (p Plan) Counts() (add, change, destroy int) {
	for _, ip := range p.Instances {
		for _, c := range ip.Changes {
			switch c.Kind {
			case Create:
				add++
			case Update:
				change++
			case Delete:
				destroy++
			}
		}
	}
	return
}

// Desired returns the objects each declared instance currently manages, by name.
func Desired(cfg *config.Config) map[string][]engine.Object {
	out := map[string][]engine.Object{}
	for _, decl := range cfg.Instances {
		drv, ok := engine.Lookup(decl.Type)
		if !ok {
			continue
		}
		inv, ok := drv.(engine.Inventory)
		if !ok {
			continue
		}
		inst := engine.Instance{Name: decl.Name, Type: decl.Type, Version: decl.Version, Spec: decl.Spec}
		if objs := inv.Objects(inst); len(objs) > 0 {
			out[decl.Name] = objs
		}
	}
	return out
}

// BuildPlan diffs the desired objects (from config) against the prior state,
// producing the changes apply would make: creates for new objects, updates for
// changed ones (different hash), deletes for objects no longer declared. Deletes
// for an instance removed from config entirely are included too.
func BuildPlan(cfg *config.Config, prior *State) Plan {
	desired := Desired(cfg)
	var plan Plan
	seen := map[string]bool{}
	for _, decl := range cfg.Instances {
		seen[decl.Name] = true
		changes := diff(prior.Objects(decl.Name), desired[decl.Name])
		if len(changes) > 0 {
			plan.Instances = append(plan.Instances, InstancePlan{Name: decl.Name, Engine: decl.Type, Changes: changes})
		}
	}
	// Instances dropped from config entirely: every applied object is deleted.
	for name, objs := range prior.Instances {
		if seen[name] {
			continue
		}
		changes := diff(objs, nil)
		if len(changes) > 0 {
			plan.Instances = append(plan.Instances, InstancePlan{Name: name, Changes: changes})
		}
	}
	return plan
}

// DestroyPlan is the plan to remove every applied object (deletes only).
func DestroyPlan(prior *State) Plan {
	var plan Plan
	for name, objs := range prior.Instances {
		changes := diff(objs, nil)
		if len(changes) > 0 {
			plan.Instances = append(plan.Instances, InstancePlan{Name: name, Changes: changes})
		}
	}
	return plan
}

// diff computes the changes from prior to desired: creates (in desired, not
// prior), updates (in both, different hash), deletes (in prior, not desired).
// Creates/updates keep desired order; deletes are appended in reverse-prior order
// so dependents drop before their dependencies.
func diff(prior, desired []engine.Object) []Change {
	priorByKey := map[string]engine.Object{}
	for _, o := range prior {
		priorByKey[key(o)] = o
	}
	desiredByKey := map[string]bool{}
	var changes []Change
	for _, o := range desired {
		desiredByKey[key(o)] = true
		if p, ok := priorByKey[key(o)]; !ok {
			changes = append(changes, Change{Kind: Create, Object: o})
		} else if p.Hash != o.Hash {
			changes = append(changes, Change{Kind: Update, Object: o})
		}
	}
	for i := len(prior) - 1; i >= 0; i-- {
		if !desiredByKey[key(prior[i])] {
			changes = append(changes, Change{Kind: Delete, Object: prior[i]})
		}
	}
	return changes
}

func key(o engine.Object) string { return o.Kind + "\x00" + o.Name }

// Removed returns the objects present in prior but not in desired, in reverse
// prior order (so dependents drop before dependencies) — the set apply prunes.
func Removed(prior, desired []engine.Object) []engine.Object {
	want := map[string]bool{}
	for _, o := range desired {
		want[key(o)] = true
	}
	var out []engine.Object
	for i := len(prior) - 1; i >= 0; i-- {
		if !want[key(prior[i])] {
			out = append(out, prior[i])
		}
	}
	return out
}

// Reverse returns objs in reverse order (drop order for a full teardown).
func Reverse(objs []engine.Object) []engine.Object {
	out := make([]engine.Object, len(objs))
	for i, o := range objs {
		out[len(objs)-1-i] = o
	}
	return out
}
