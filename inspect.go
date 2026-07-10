package doze

import (
	"context"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/state"
)

// Inspection is a daemon-less view of a stack: parse + decode a config (or a
// programmatic Stack) and reason about it — validate, read the topology, and
// compute the reconciliation plan — without booting anything. It is how you
// build `doze lint`, `doze tree`, or a config viewer on the library.
//
// Load returns an error when the config is invalid (that IS the lint result);
// a non-nil Inspection is always valid. Call Close to release the engine host.
type Inspection struct {
	cfg  *config.Config
	host interface{ Close() error }
}

// Load parses and validates the config (or Stack) in Options WITHOUT starting a
// daemon. A returned error is the validation failure. Close the Inspection when
// done.
func Load(opts Options) (*Inspection, error) {
	cfg, host, _, err := loadHostAndConfig(opts)
	if err != nil {
		return nil, err
	}
	return &Inspection{cfg: cfg, host: host}, nil
}

// Close releases the engine host (reaps plugin processes).
func (in *Inspection) Close() error { return in.host.Close() }

// StackName returns the stack name.
func (in *Inspection) StackName() string { return in.cfg.Stack() }

// Services returns the declared instance names, in declaration order.
func (in *Inspection) Services() []string {
	out := make([]string, 0, len(in.cfg.Instances))
	for _, d := range in.cfg.Instances {
		out = append(out, d.Name)
	}
	return out
}

// Topology returns the declared instance graph as data.
func (in *Inspection) Topology() []Node { return topologyOf(in.cfg) }

// Plan computes the structural changes a Sync would make — creates for new
// declared objects, updates for changed ones, deletes for objects (or whole
// instances) no longer declared — by diffing the config against the last
// applied state on disk. Daemon-less.
func (in *Inspection) Plan() (Plan, error) { return planOf(in.cfg) }

// --- plan types ---

// Plan is the set of structural changes reconciliation would make, grouped by
// instance.
type Plan struct {
	Instances []InstanceChanges
}

// InstanceChanges is one instance's slice of a Plan.
type InstanceChanges struct {
	Name    string
	Engine  string
	Changes []ObjectChange
}

// ObjectChange is a single create/update/delete of a declared object (a
// database, role, bucket, queue, …).
type ObjectChange struct {
	Action string // "create" | "update" | "delete"
	Kind   string // object kind: "database", "role", "bucket", …
	Name   string // object name
}

// Empty reports whether the plan makes no changes.
func (p Plan) Empty() bool {
	for _, ic := range p.Instances {
		if len(ic.Changes) > 0 {
			return false
		}
	}
	return true
}

// Counts returns the number of objects to add, change, and destroy.
func (p Plan) Counts() (add, change, destroy int) {
	for _, ic := range p.Instances {
		for _, c := range ic.Changes {
			switch c.Action {
			case "create":
				add++
			case "update":
				change++
			case "delete":
				destroy++
			}
		}
	}
	return
}

// planOf builds the public Plan from the internal diff engine.
func planOf(cfg *config.Config) (Plan, error) {
	prior, err := state.Load(state.Path(cfg.Path()))
	if err != nil {
		return Plan{}, err
	}
	sp := state.BuildPlan(cfg, prior)
	var out Plan
	for _, ip := range sp.Instances {
		if len(ip.Changes) == 0 {
			continue
		}
		ic := InstanceChanges{Name: ip.Name, Engine: ip.Engine}
		for _, c := range ip.Changes {
			ic.Changes = append(ic.Changes, ObjectChange{
				Action: actionName(c.Kind),
				Kind:   c.Object.Kind,
				Name:   c.Object.Name,
			})
		}
		out.Instances = append(out.Instances, ic)
	}
	return out, nil
}

func actionName(k state.ChangeKind) string {
	switch k {
	case state.Create:
		return "create"
	case state.Update:
		return "update"
	default:
		return "delete"
	}
}

// topologyOf projects the declared graph (shared by Inspection and Session).
func topologyOf(cfg *config.Config) []Node {
	out := make([]Node, 0, len(cfg.Instances))
	for _, d := range cfg.Instances {
		deps := make([]string, 0, len(d.Deps))
		for _, dep := range d.Deps {
			deps = append(deps, dep.Name)
		}
		out = append(out, Node{
			Name:      d.Name,
			Engine:    d.Type,
			Version:   d.Version.String(),
			Port:      d.Port,
			Enabled:   d.Enabled,
			DependsOn: deps,
		})
	}
	return out
}

// --- reconciliation on a live session ---

// SyncOptions configures Sync.
type SyncOptions struct {
	// Only limits the sync to a single instance (empty = the whole stack).
	Only string
	// DryRun computes and returns the plan without applying it.
	DryRun bool
}

// Plan computes what Sync would change, without applying it — the same diff the
// daemon-less Inspection.Plan returns, for a live session.
func (s *Session) Plan(ctx context.Context) (Plan, error) { return planOf(s.cfg) }

// Sync reconciles the stack with its config: it creates newly-declared
// structure, updates changed structure, and prunes structure that was applied
// before but is no longer declared (data is preserved). It returns the plan it
// carried out (or, with DryRun, would carry out).
func (s *Session) Sync(ctx context.Context, opts SyncOptions) (Plan, error) {
	plan, err := planOf(s.cfg)
	if err != nil {
		return Plan{}, err
	}
	if opts.DryRun || plan.Empty() {
		return plan, nil
	}
	if err := s.backend.op(ctx, "apply", opts.Only); err != nil {
		return plan, opError("sync", firstNonEmpty(opts.Only, s.cfg.Stack()), err)
	}
	return plan, nil
}
