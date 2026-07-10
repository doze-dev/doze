package doze

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Stack builds a doze stack programmatically — the config-less alternative to
// an HCL file. Add services with AddProcess (the built-in supervised-process
// engine, typed) and AddModule (any module engine — postgres, valkey, kafka,
// s3, … — whose body is expressed as HCL since it is decoded out-of-process).
// Pass the Stack to Serve/Attach via Options.Stack, or render it with HCL().
//
// A Stack renders to a canonical HCL document and is parsed through the exact
// same pipeline as a file, so validation, references, and plugin decode behave
// identically — and HCL() gives you the equivalent file to commit if you want.
type Stack struct {
	name        string
	domains     *bool
	idleTimeout time.Duration
	blocks      []blockRenderer
}

type blockRenderer interface {
	instanceName() string
	render(b *strings.Builder)
}

// NewStack starts a stack with the given name (the stack/domain label).
func NewStack(name string) *Stack { return &Stack{name: name} }

// Domains toggles per-service local DNS names (<name>.<stack>.doze).
func (s *Stack) Domains(on bool) *Stack { s.domains = &on; return s }

// IdleTimeout sets how long an idle backend lives before the reaper stops it.
func (s *Stack) IdleTimeout(d time.Duration) *Stack { s.idleTimeout = d; return s }

// AddProcess adds a supervised process (the first-class core engine). It
// returns the Stack for chaining.
func (s *Stack) AddProcess(name string, p Process) *Stack {
	s.blocks = append(s.blocks, &processBlock{name: name, p: p})
	return s
}

// AddModule adds a module engine instance (postgres, valkey, kafka, s3, …). The
// returned *Module is configured fluently; its engine-specific settings are
// HCL, either via Set (scalars) or Body (sub-blocks and cross-service refs).
func (s *Stack) AddModule(engine, name string) *Module {
	m := NewModule(engine, name)
	s.blocks = append(s.blocks, m)
	return m
}

// NewModule builds a standalone module-instance spec, for live Session.AddModule
// (outside a Stack). Configure it fluently, then pass it to Session.AddModule.
func NewModule(engine, name string) *Module {
	return &Module{engine: engine, name: name}
}

// blockHCL renders a single instance block (no stack name/defaults), for the
// live "add" control op.
func (m *Module) blockHCL() string {
	var b strings.Builder
	m.render(&b)
	return b.String()
}

func (p Process) blockHCL(name string) string {
	var b strings.Builder
	(&processBlock{name: name, p: p}).render(&b)
	return b.String()
}

// HCL renders the stack to the equivalent HCL document.
func (s *Stack) HCL() string {
	var b strings.Builder
	if s.name != "" {
		fmt.Fprintf(&b, "name = %s\n\n", hclString(s.name))
	}
	if s.domains != nil || s.idleTimeout != 0 {
		b.WriteString("defaults {\n")
		if s.idleTimeout != 0 {
			fmt.Fprintf(&b, "  idle_timeout = %s\n", hclString(s.idleTimeout.String()))
		}
		if s.domains != nil {
			fmt.Fprintf(&b, "  domains = %s\n", strconv.FormatBool(*s.domains))
		}
		b.WriteString("}\n\n")
	}
	for _, blk := range s.blocks {
		blk.render(&b)
		b.WriteString("\n")
	}
	return b.String()
}

// --- module builder ---

// Module is a fluent builder for a module-engine instance in a Stack.
type Module struct {
	engine, name string
	version      string
	listen       string
	group        string
	port         int
	enabled      *bool
	dependsOn    []string
	attrs        []kv     // ordered engine-specific scalar attributes (Set)
	bodies       []string // raw HCL body fragments (Body)
}

type kv struct {
	key string
	val string // pre-rendered HCL value
}

// Version pins the engine major/exact version.
func (m *Module) Version(v string) *Module { m.version = v; return m }

// Port sets the client-facing port.
func (m *Module) Port(p int) *Module { m.port = p; return m }

// Listen sets a full host:port (or unix path) listen override.
func (m *Module) Listen(addr string) *Module { m.listen = addr; return m }

// Group overrides the display group in status/dash.
func (m *Module) Group(g string) *Module { m.group = g; return m }

// Enabled toggles the instance (false = declared but paused).
func (m *Module) Enabled(b bool) *Module { m.enabled = &b; return m }

// DependsOn adds explicit dependencies (booted first).
func (m *Module) DependsOn(names ...string) *Module {
	m.dependsOn = append(m.dependsOn, names...)
	return m
}

// Set adds one engine-specific scalar/list attribute (string, bool, int/float,
// []string, or map[string]string), rendered as HCL.
func (m *Module) Set(key string, value any) *Module {
	m.attrs = append(m.attrs, kv{key: key, val: hclValue(value)})
	return m
}

// Body appends a raw HCL body fragment — the way to express nested blocks
// (role "app" {}, target { arn = sqs.jobs.arn }) and cross-service references.
func (m *Module) Body(hcl string) *Module {
	m.bodies = append(m.bodies, strings.TrimRight(hcl, "\n"))
	return m
}

func (m *Module) instanceName() string { return m.name }

func (m *Module) render(b *strings.Builder) {
	fmt.Fprintf(b, "%s %s {\n", m.engine, hclString(m.name))
	if m.version != "" {
		fmt.Fprintf(b, "  version = %s\n", hclString(m.version))
	}
	if m.port != 0 {
		fmt.Fprintf(b, "  port = %d\n", m.port)
	}
	if m.listen != "" {
		fmt.Fprintf(b, "  listen = %s\n", hclString(m.listen))
	}
	if m.group != "" {
		fmt.Fprintf(b, "  group = %s\n", hclString(m.group))
	}
	if m.enabled != nil {
		fmt.Fprintf(b, "  enabled = %s\n", strconv.FormatBool(*m.enabled))
	}
	if len(m.dependsOn) > 0 {
		fmt.Fprintf(b, "  depends_on = %s\n", hclDependsMap(m.dependsOn))
	}
	for _, a := range m.attrs {
		fmt.Fprintf(b, "  %s = %s\n", a.key, a.val)
	}
	for _, body := range m.bodies {
		writeIndented(b, body)
	}
	b.WriteString("}\n")
}

// --- process block ---

// Process is the typed configuration of a supervised process, mirroring the
// `process` block. Command is required.
type Process struct {
	Command   string
	Cwd       string
	Port      int
	Ingress   bool
	Forward   int // forward port for an ingress process
	Env       map[string]string
	EnvFile   string
	Health    *Health
	Restart   *Restart
	Hooks     *Hooks
	Group     string
	DependsOn []string
	Enabled   *bool // default true
}

// Health is a process readiness/liveness probe (set exactly one of the targets).
type Health struct {
	HTTP     string
	TCP      string
	Exec     string
	LogLine  string
	Interval string // Go duration string, e.g. "2s"
	Timeout  string
	Retries  int
}

// Restart is the supervisor restart policy for an unexpected exit.
type Restart struct {
	Policy     string // "always" | "on-failure" | "never"
	Backoff    string // Go duration string
	MaxRetries int
}

// Hooks are lifecycle command hooks.
type Hooks struct {
	PreStart  []string
	PostStart []string
	PreStop   []string
}

type processBlock struct {
	name string
	p    Process
}

func (pb *processBlock) instanceName() string { return pb.name }

func (pb *processBlock) render(b *strings.Builder) {
	p := pb.p
	fmt.Fprintf(b, "process %s {\n", hclString(pb.name))
	fmt.Fprintf(b, "  command = %s\n", hclString(p.Command))
	if p.Cwd != "" {
		fmt.Fprintf(b, "  cwd = %s\n", hclString(p.Cwd))
	}
	if p.Port != 0 {
		fmt.Fprintf(b, "  port = %d\n", p.Port)
	}
	if p.Ingress {
		b.WriteString("  ingress = true\n")
	}
	if p.Forward != 0 {
		fmt.Fprintf(b, "  forward = %d\n", p.Forward)
	}
	if p.EnvFile != "" {
		fmt.Fprintf(b, "  env_file = %s\n", hclString(p.EnvFile))
	}
	if len(p.Env) > 0 {
		fmt.Fprintf(b, "  env = %s\n", hclStringMap(p.Env))
	}
	if p.Group != "" {
		fmt.Fprintf(b, "  group = %s\n", hclString(p.Group))
	}
	if p.Enabled != nil {
		fmt.Fprintf(b, "  enabled = %s\n", strconv.FormatBool(*p.Enabled))
	}
	if len(p.DependsOn) > 0 {
		fmt.Fprintf(b, "  depends_on = %s\n", hclDependsMap(p.DependsOn))
	}
	if h := p.Health; h != nil {
		b.WriteString("  health {\n")
		writeOptAttr(b, "    ", "http", h.HTTP)
		writeOptAttr(b, "    ", "tcp", h.TCP)
		writeOptAttr(b, "    ", "exec", h.Exec)
		writeOptAttr(b, "    ", "log_line", h.LogLine)
		writeOptAttr(b, "    ", "interval", h.Interval)
		writeOptAttr(b, "    ", "timeout", h.Timeout)
		if h.Retries != 0 {
			fmt.Fprintf(b, "    retries = %d\n", h.Retries)
		}
		b.WriteString("  }\n")
	}
	if r := p.Restart; r != nil {
		b.WriteString("  restart {\n")
		writeOptAttr(b, "    ", "policy", r.Policy)
		writeOptAttr(b, "    ", "backoff", r.Backoff)
		if r.MaxRetries != 0 {
			fmt.Fprintf(b, "    max_retries = %d\n", r.MaxRetries)
		}
		b.WriteString("  }\n")
	}
	if hk := p.Hooks; hk != nil {
		b.WriteString("  hooks {\n")
		if len(hk.PreStart) > 0 {
			fmt.Fprintf(b, "    pre_start = %s\n", hclStringList(hk.PreStart))
		}
		if len(hk.PostStart) > 0 {
			fmt.Fprintf(b, "    post_start = %s\n", hclStringList(hk.PostStart))
		}
		if len(hk.PreStop) > 0 {
			fmt.Fprintf(b, "    pre_stop = %s\n", hclStringList(hk.PreStop))
		}
		b.WriteString("  }\n")
	}
	b.WriteString("}\n")
}

// --- HCL value encoding ---

func writeOptAttr(b *strings.Builder, indent, key, val string) {
	if val != "" {
		fmt.Fprintf(b, "%s%s = %s\n", indent, key, hclString(val))
	}
}

func writeIndented(b *strings.Builder, body string) {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			b.WriteString("\n")
			continue
		}
		fmt.Fprintf(b, "  %s\n", strings.TrimRight(line, " \t"))
	}
}

// hclValue renders a Go value as an HCL literal.
func hclValue(v any) string {
	switch t := v.(type) {
	case string:
		return hclString(t)
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case []string:
		return hclStringList(t)
	case map[string]string:
		return hclStringMap(t)
	default:
		return hclString(fmt.Sprint(v))
	}
}

func hclString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s) + `"`
}

// hclDependsMap renders depends_on as HCL: a map of instance name → readiness
// condition. doze's depends_on is `{ "name" = "healthy" }`, not a list; the
// default condition is "healthy" (the runtime waits for Healthy regardless).
func hclDependsMap(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("%s = %q", hclIdent(n), "healthy")
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func hclStringList(items []string) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = hclString(it)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func hclStringMap(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{ ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s = %s", hclIdent(k), hclString(m[k]))
	}
	b.WriteString(" }")
	return b.String()
}

// hclIdent quotes a map key when it isn't a bare identifier.
func hclIdent(k string) string {
	bare := k != ""
	for _, r := range k {
		if !(r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			bare = false
			break
		}
	}
	if bare {
		return k
	}
	return hclString(k)
}
