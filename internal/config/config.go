// Package config parses doze.hcl into a validated, engine-agnostic view.
//
// The root (listen, home, data_dir, defaults, tls) is fixed; each database
// engine contributes its own block type (postgres, valkey, …). For every block
// whose keyword matches a registered engine driver, config reads the common
// fields (version, listen) and hands the rest of the block body to the driver's
// ConfigDecoder, so config itself knows nothing engine-specific.
package config

import (
	"time"

	"github.com/doze-dev/doze-sdk/engine"
)

// Defaults for fields the user did not specify.
const (
	DefaultListen      = "127.0.0.1:6432"
	DefaultIdleTimeout = 5 * time.Minute
)

// Config is the validated, typed view of a doze.hcl file.
type Config struct {
	// Listen is the default client-facing address; instances may override it.
	Listen string
	// Home is the global doze home (shared toolchains + cache), deduped across
	// projects. Resolved from `home`, then $DOZE_HOME, then ~/.doze.
	Home string
	// DataDir is this project's state directory; defaults to
	// <Home>/projects/<slug> so projects never collide.
	DataDir string
	// StackName names this stack — the `name = "shop"` root attribute, defaulting
	// to the config directory's name. It scopes local domains
	// (<service>.<stack>.doze) and must be unique among the machine's
	// running stacks (the daemon enforces it via the shared stack registry).
	StackName string
	// Defaults is the generic tuning profile (engine-agnostic).
	Defaults Defaults
	// TLS configures client-facing TLS termination.
	TLS TLSSettings
	// Modules configures the out-of-process engine plugin fetcher (source + pins).
	Modules ModulesConfig
	// Instances preserves declaration order from the file.
	Instances []*InstanceDecl
	// Outputs are the declared output values, keyed by name (declaration order in
	// OutputOrder), resolved against the final evaluation context.
	Outputs     map[string]Output
	OutputOrder []string

	path  string
	index map[string]*InstanceDecl
}

// Output is a declared output value: the connection strings or facts a stack
// exposes. Evaluated during `doze sync` and recorded in the project state.
type Output struct {
	Name        string
	Value       string // rendered value
	Description string
	Sensitive   bool
}

// Defaults holds engine-agnostic tuning. Engine-specific tuning (Postgres
// shared_buffers, fsync, …) lives inside that engine's config block.
type Defaults struct {
	IdleTimeout time.Duration
	// Domains publishes a local DNS name for every enabled instance with a
	// port — <service>.<stack>.doze → 127.0.0.1, answered by the daemon's
	// built-in resolver (with /etc/resolver/doze pointing at it) — so
	// connection strings read as postgres://…@orders-pg.demo.doze:5432
	// instead of a bare loopback address.
	Domains bool
}

// TLSSettings configures TLS termination between clients and the proxy.
type TLSSettings struct {
	Enabled  bool
	Cert     string
	Key      string
	Required bool
}

// InstanceDecl is one declared instance: a database server of some engine.
type InstanceDecl struct {
	Type    string              // engine type / block keyword ("postgres")
	Name    string              // block label
	Version engine.VersionSpec  // "16" (major) or "16.14" (exact)
	Listen  string              // optional full endpoint override ("host:port")
	Port    int                 // the client-facing port (required unless Listen is set)
	Spec    engine.EngineConfig // engine-specific config (decoded by the driver)
	// Deps are the other declared instances this one must boot first (e.g. an sns
	// instance referencing sqs.jobs), each with a readiness condition. Derived
	// from the config reference graph (every reference is a Healthy dependency)
	// and any explicit `depends_on`; the runtime boots and holds them first.
	Index int                 // declaration order, used for endpoint address assignment
	Deps  []engine.Dependency // dependencies, in reference order
	// Enabled defaults to true; `enabled = false` declares the instance but leaves it
	// paused — not booted by up/wake, not converged or pruned by sync (its data is
	// preserved), shown as "disabled" in the tree. Re-enabling brings it back as-is.
	Enabled bool
}

// ModulesConfig is the decoded `modules {}` block: where to fetch out-of-process
// engine plugins from, per-engine source overrides, and (rarely) per-engine
// exact module-version pins. Module selection is otherwise automatic — the
// newest release compatible with this doze and the declared engine versions —
// and doze.lock freezes it, so reproducibility costs no cognitive load.
type ModulesConfig struct {
	Mirror   string            // registry base (overrides DOZE_MODULES_MIRROR)
	Enabled  bool              // fetch plugin modules (true also when a mirror is set)
	Sources  map[string]string // engine type -> source address override ("" = doze/<type>)
	Versions map[string]string // engine type -> exact module version pin ("" = auto)
}

// Lookup returns the declared instance by name, or nil.
func (c *Config) Lookup(name string) *InstanceDecl { return c.index[name] }

// Add registers an additional instance at runtime, for synthetic instances that
// are not declared in the config file.
func (c *Config) Add(decl *InstanceDecl) {
	if c.index == nil {
		c.index = map[string]*InstanceDecl{}
	}
	c.Instances = append(c.Instances, decl)
	c.index[decl.Name] = decl
}

// Remove deletes a runtime-added (or declared) instance by name, returning
// whether it was present. Used by live Remove.
func (c *Config) Remove(name string) bool {
	if _, ok := c.index[name]; !ok {
		return false
	}
	delete(c.index, name)
	for i, d := range c.Instances {
		if d.Name == name {
			c.Instances = append(c.Instances[:i], c.Instances[i+1:]...)
			break
		}
	}
	return true
}

// Path is the file this config was loaded from (empty for in-memory parses).
func (c *Config) Path() string { return c.path }
