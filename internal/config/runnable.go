// `service` and `task` blocks: declared, checked and published for references.
// Nothing runs them yet — see Config.Unbuilt.
package config

import (
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/runspec"
)

// Ref points at a declared block: `task.migrate` is {KindTask, "migrate"}.
type Ref struct {
	Kind string
	Name string
}

func (r Ref) String() string { return r.Kind + "." + r.Name }

// ServiceDecl is one `service "<name>" { … }` block: a long-running command
// from outside the doze registry.
type ServiceDecl struct {
	Name    string
	Run     string
	Dir     string // absolute
	Port    int    // 0: the service has no port
	Env     map[string]string
	EnvFile string // absolute, "" if none
	// Download is what to fetch before the first run, keyed by platform (mac,
	// mac_intel, linux, linux_arm). The single key "" is one URL for all of them.
	Download     map[string]string
	Setup        []string
	OnReady      []string
	Stop         []string
	Ready        *runspec.Probe // nil: tcp() when Port is set
	ReadyTimeout time.Duration
	Restart      RestartDecl
	Sleep        runspec.Sleep
	Front        string
	Console      string
	QuietLogs    bool
	After        []Ref
	Refs         []Ref // what the body references, in source order

	block *hcl.Block
}

// TaskDecl is one `task "<name>" { … }` block: a command that runs to
// completion.
type TaskDecl struct {
	Name    string
	Run     string
	Dir     string // absolute
	Env     map[string]string
	EnvFile string
	Inputs  []string // globs; the task re-runs when the content they match changes
	Manual  bool     // only runs when asked
	After   []Ref
	Refs    []Ref

	block *hcl.Block
}

// RestartDecl is a `restart { … }` block as written; empty fields take the
// engine's defaults.
type RestartDecl struct {
	Policy     string
	Backoff    time.Duration
	MaxRetries int
}

type hclRestart struct {
	Policy     string `hcl:"policy,optional"`
	Backoff    string `hcl:"backoff,optional"`
	MaxRetries int    `hcl:"max_retries,optional"`
}

type hclService struct {
	Run          string            `hcl:"run"`
	Dir          string            `hcl:"dir,optional"`
	Port         int               `hcl:"port,optional"`
	Env          map[string]string `hcl:"env,optional"`
	EnvFile      string            `hcl:"env_file,optional"`
	Download     cty.Value         `hcl:"download,optional"`
	Setup        cty.Value         `hcl:"setup,optional"`
	OnReady      cty.Value         `hcl:"on_ready,optional"`
	Stop         cty.Value         `hcl:"stop,optional"`
	Ready        cty.Value         `hcl:"ready,optional"`
	ReadyTimeout string            `hcl:"ready_timeout,optional"`
	Restart      *hclRestart       `hcl:"restart,block"`
	Sleep        string            `hcl:"sleep,optional"`
	Front        string            `hcl:"front,optional"`
	Console      string            `hcl:"console,optional"`
	Logs         string            `hcl:"logs,optional"`
}

type hclTask struct {
	Run     string            `hcl:"run"`
	Dir     string            `hcl:"dir,optional"`
	Env     map[string]string `hcl:"env,optional"`
	EnvFile string            `hcl:"env_file,optional"`
	Inputs  []string          `hcl:"inputs,optional"`
	Manual  bool              `hcl:"manual,optional"`
}

// downloadPlatforms are the keys of the map form of `download`.
var downloadPlatforms = []string{"mac", "mac_intel", "linux", "linux_arm"}

// Service returns the declared service by name, or nil.
func (c *Config) Service(name string) *ServiceDecl {
	for _, s := range c.Services {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// Task returns the declared task by name, or nil.
func (c *Config) Task(name string) *TaskDecl {
	for _, t := range c.Tasks {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// Unbuilt reports the blocks this build of doze reads but cannot run. A
// `service` or a `task` is checked by `doze lint`; running one arrives with the
// lifecycle work (docs/design/next/phases.md, phase 4). nil means the stack is
// runnable.
func (c *Config) Unbuilt() error {
	var names []string
	for _, s := range c.Services {
		if len(s.Download) > 0 {
			names = append(names, title(KindService, "", s.Name)+" (download)")
		}
	}
	for _, t := range c.Tasks {
		names = append(names, title(KindTask, "", t.Name))
	}
	if len(names) == 0 {
		return nil
	}
	return fmt.Errorf("this build of doze checks `service` and `task` blocks but cannot run them yet: %s", strings.Join(names, ", "))
}

// declareRunnable registers a service or task by name and, for a service,
// reads the one argument other blocks' references need before anything else is
// decoded: its port.
func (l *loader) declareRunnable(cfg *Config, block *hcl.Block, declRanges map[string]hcl.Range, ctx *hcl.EvalContext) error {
	name := block.Labels[0]
	if first, dup := declRanges[name]; dup {
		return posErr(l.parser, block.DefRange,
			fmt.Sprintf("%s: the name %q is already taken", title(block.Type, "", name), name),
			"first declared at "+first.String()+" — names are unique across a stack, whatever the kind")
	}
	declRanges[name] = block.DefRange
	if block.Type == KindTask {
		cfg.Tasks = append(cfg.Tasks, &TaskDecl{Name: name, block: block})
		return nil
	}
	svc := &ServiceDecl{Name: name, block: block}
	head, _, _ := block.Body.PartialContent(&hcl.BodySchema{Attributes: []hcl.AttributeSchema{{Name: "port"}}})
	if attr, ok := head.Attributes["port"]; ok {
		if diags := gohcl.DecodeExpression(attr.Expr, ctx, &svc.Port); diags.HasErrors() {
			return diagError(l.parser, diags)
		}
		if svc.Port < 1 || svc.Port > 65535 {
			return posErr(l.parser, attr.Range, fmt.Sprintf("%s: port %d is out of range", title(KindService, "", name), svc.Port),
				"a port must be between 1 and 65535")
		}
	}
	cfg.Services = append(cfg.Services, svc)
	return nil
}

// publishRunnables exposes every service and task to references, as
// service.<name> and task.<name>.
func (cfg *Config) publishRunnables(ctx *hcl.EvalContext) {
	services := map[string]cty.Value{}
	for _, s := range cfg.Services {
		host := "127.0.0.1"
		if cfg.Defaults.Domains {
			host = cfg.DomainFor(s.Name)
		}
		address := ""
		if s.Port != 0 {
			address = net.JoinHostPort(host, strconv.Itoa(s.Port))
		}
		services[s.Name] = cty.ObjectVal(map[string]cty.Value{
			"name":    cty.StringVal(s.Name),
			"address": cty.StringVal(address),
			"host":    cty.StringVal(host),
			"port":    cty.NumberIntVal(int64(s.Port)),
			"data":    cty.StringVal(cfg.ClusterDir(s.Name)),
		})
	}
	tasks := map[string]cty.Value{}
	for _, t := range cfg.Tasks {
		tasks[t.Name] = cty.ObjectVal(map[string]cty.Value{"name": cty.StringVal(t.Name)})
	}
	ctx.Variables[KindService] = cty.ObjectVal(services)
	ctx.Variables[KindTask] = cty.ObjectVal(tasks)
}

// decodeRunnables decodes every service and task body. It runs after the
// modules and apps are evaluated, so a body may reference any of them.
func (l *loader) decodeRunnables(cfg *Config, ctx *hcl.EvalContext, known *declared) error {
	// The probe constructors exist only while these bodies decode.
	rctx := ctx.NewChild()
	rctx.Functions = runspec.Functions()
	for _, s := range cfg.Services {
		if err := l.decodeService(cfg, s, rctx, known); err != nil {
			return err
		}
	}
	for _, t := range cfg.Tasks {
		if err := l.decodeTask(cfg, t, rctx, known); err != nil {
			return err
		}
	}
	return nil
}

// splitAfter strips `after` off a body (it holds references, not a value) and
// resolves it and the body's own references.
func (l *loader) splitAfter(block *hcl.Block, self string, known *declared) (rest hcl.Body, after, refs []Ref, err error) {
	meta, rest, diags := block.Body.PartialContent(&hcl.BodySchema{Attributes: []hcl.AttributeSchema{{Name: "after"}}})
	if diags.HasErrors() {
		return nil, nil, nil, diagError(l.parser, diags)
	}
	if attr, ok := meta.Attributes["after"]; ok {
		if after, err = parseAfter(l.parser, attr, known); err != nil {
			return nil, nil, nil, err
		}
	}
	for _, t := range referencedTraversals(rest) {
		ref, ok, err := known.resolve(l.parser, t)
		if err != nil {
			return nil, nil, nil, err
		}
		if ok && ref.Name != self {
			refs = append(refs, ref)
		}
	}
	return rest, after, refs, nil
}

func (l *loader) decodeService(cfg *Config, s *ServiceDecl, ctx *hcl.EvalContext, known *declared) error {
	rest, after, refs, err := l.splitAfter(s.block, s.Name, known)
	if err != nil {
		return err
	}
	var raw hclService
	if diags := gohcl.DecodeBody(rest, ctx, &raw); diags.HasErrors() {
		return diagError(l.parser, diags)
	}
	fail := func(err error) error {
		return posErr(l.parser, s.block.DefRange, title(KindService, "", s.Name)+": "+err.Error(), "")
	}
	if strings.TrimSpace(raw.Run) == "" {
		return fail(fmt.Errorf("`run` must not be empty"))
	}
	baseDir := blockBaseDir(cfg, s.block)
	s.Run, s.Env, s.Dir = raw.Run, raw.Env, resolveAgainst(baseDir, raw.Dir)
	s.After, s.Refs = after, refs
	s.Front, s.Console = raw.Front, raw.Console
	if raw.EnvFile != "" {
		s.EnvFile = resolveAgainst(baseDir, raw.EnvFile)
	}
	if s.Download, err = parseDownload(raw.Download); err != nil {
		return fail(err)
	}
	if s.Setup, err = runspec.Steps("setup", raw.Setup); err != nil {
		return fail(err)
	}
	if s.OnReady, err = runspec.Steps("on_ready", raw.OnReady); err != nil {
		return fail(err)
	}
	if s.Stop, err = runspec.Steps("stop", raw.Stop); err != nil {
		return fail(err)
	}
	if s.Ready, err = runspec.ParseReady(raw.Ready); err != nil {
		return fail(err)
	}
	if s.Ready != nil && s.Ready.NeedsPort() && s.Port == 0 {
		return fail(fmt.Errorf("ready = %s(…) needs a `port` to probe", s.Ready.Kind))
	}
	if s.ReadyTimeout, err = runspec.ParseReadyTimeout(raw.ReadyTimeout); err != nil {
		return fail(err)
	}
	if s.Sleep, err = runspec.ParseSleep(raw.Sleep); err != nil {
		return fail(err)
	}
	if s.QuietLogs, err = runspec.ParseLogs(raw.Logs); err != nil {
		return fail(err)
	}
	if err := runspec.CheckConsole(raw.Console); err != nil {
		return fail(err)
	}
	if s.Restart, err = parseRestart(raw.Restart); err != nil {
		return fail(err)
	}
	return cfg.addService(s, fail)
}

// addService makes a decoded service a declared instance of the command
// engine, so it is booted, held, slept and shown like any other. The block's
// references and `after` become boot edges.
func (cfg *Config) addService(s *ServiceDecl, fail func(error) error) error {
	drv, ok := engine.Lookup(AppEngine)
	if !ok {
		return fail(fmt.Errorf("the command engine is not registered"))
	}
	b, ok := drv.(runspec.CommandBuilder)
	if !ok {
		return fail(fmt.Errorf("the command engine cannot build a service"))
	}
	spec, err := b.CommandConfig(runspec.Command{
		Name: s.Name, Run: s.Run, Dir: s.Dir, Port: s.Port, Env: s.Env, EnvFile: s.EnvFile,
		Setup: s.Setup, OnReady: s.OnReady, Stop: s.Stop,
		Ready: s.Ready, ReadyTimeout: s.ReadyTimeout,
		RestartPolicy: s.Restart.Policy, RestartBackoff: s.Restart.Backoff, RestartMaxRetries: s.Restart.MaxRetries,
		Sleep: s.Sleep, Front: s.Front, Console: s.Console, QuietLogs: s.QuietLogs,
		Service: true,
	})
	if err != nil {
		return fail(err)
	}
	var deps []engine.Dependency
	seen := map[string]bool{s.Name: true}
	for _, ref := range append(append([]Ref{}, s.Refs...), s.After...) {
		if seen[ref.Name] || (ref.Kind != KindModule && ref.Kind != KindApp && ref.Kind != KindService) {
			continue
		}
		seen[ref.Name] = true
		deps = append(deps, engine.Dependency{Name: ref.Name, Condition: engine.Healthy})
	}
	cfg.Add(&InstanceDecl{
		Kind: KindService, Type: AppEngine, Name: s.Name, Version: "builtin",
		Port: s.Port, Spec: spec, Index: len(cfg.Instances), Deps: deps, Enabled: true,
		After: s.After, Refs: s.Refs,
	})
	return nil
}

func (l *loader) decodeTask(cfg *Config, t *TaskDecl, ctx *hcl.EvalContext, known *declared) error {
	rest, after, refs, err := l.splitAfter(t.block, t.Name, known)
	if err != nil {
		return err
	}
	var raw hclTask
	if diags := gohcl.DecodeBody(rest, ctx, &raw); diags.HasErrors() {
		return diagError(l.parser, diags)
	}
	if strings.TrimSpace(raw.Run) == "" {
		return posErr(l.parser, t.block.DefRange, title(KindTask, "", t.Name)+": `run` must not be empty", "")
	}
	baseDir := blockBaseDir(cfg, t.block)
	t.Run, t.Env, t.Dir = raw.Run, raw.Env, resolveAgainst(baseDir, raw.Dir)
	t.Inputs, t.Manual = raw.Inputs, raw.Manual
	t.After, t.Refs = after, refs
	if raw.EnvFile != "" {
		t.EnvFile = resolveAgainst(baseDir, raw.EnvFile)
	}
	return nil
}

// parseDownload reads `download`: one URL, or a map of platform to URL.
func parseDownload(v cty.Value) (map[string]string, error) {
	if v.IsNull() {
		return nil, nil
	}
	bad := fmt.Errorf("download takes a URL, or a map of platform (%s) to URL", strings.Join(downloadPlatforms, ", "))
	if !v.IsKnown() {
		return nil, bad
	}
	checkURL := func(u string) error {
		if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
			return fmt.Errorf("download = %q: want an http:// or https:// URL", u)
		}
		return nil
	}
	if v.Type() == cty.String {
		if err := checkURL(v.AsString()); err != nil {
			return nil, err
		}
		return map[string]string{"": v.AsString()}, nil
	}
	if !v.Type().IsObjectType() && !v.Type().IsMapType() {
		return nil, bad
	}
	out := map[string]string{}
	for it := v.ElementIterator(); it.Next(); {
		k, e := it.Element()
		if e.IsNull() || e.Type() != cty.String {
			return nil, bad
		}
		platform := k.AsString()
		ok := false
		for _, p := range downloadPlatforms {
			ok = ok || p == platform
		}
		if !ok {
			return nil, fmt.Errorf("download: %q is not a platform (want %s)", platform, strings.Join(downloadPlatforms, ", "))
		}
		if err := checkURL(e.AsString()); err != nil {
			return nil, err
		}
		out[platform] = e.AsString()
	}
	if len(out) == 0 {
		return nil, bad
	}
	return out, nil
}

func parseRestart(r *hclRestart) (RestartDecl, error) {
	if r == nil {
		return RestartDecl{}, nil
	}
	out := RestartDecl{Policy: r.Policy, MaxRetries: r.MaxRetries}
	switch r.Policy {
	case "", "no", "on_failure", "always":
	default:
		return out, fmt.Errorf("invalid restart policy %q (want no, on_failure, or always)", r.Policy)
	}
	if r.Backoff != "" {
		d, err := time.ParseDuration(r.Backoff)
		if err != nil || d < 0 {
			return out, fmt.Errorf("invalid restart backoff %q", r.Backoff)
		}
		out.Backoff = d
	}
	return out, nil
}

// parseAfter reads `after = [task.migrate, module.db]`: a list of references
// to whole blocks, each of which must be declared.
func parseAfter(parser *hclparse.Parser, attr *hcl.Attribute, known *declared) ([]Ref, error) {
	const want = "after takes a list of references to blocks, for example: after = [task.migrate, module.db]"
	exprs, diags := hcl.ExprList(attr.Expr)
	if diags.HasErrors() {
		return nil, posErr(parser, attr.Range, "invalid after", want)
	}
	var out []Ref
	for _, e := range exprs {
		t, diags := hcl.AbsTraversalForExpr(e)
		if diags.HasErrors() || len(t) != 2 || !isKind(t.RootName()) {
			return nil, posErr(parser, e.Range(), "invalid after", want)
		}
		ref, _, err := known.resolve(parser, t)
		if err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, nil
}

// blockBaseDir is the absolute directory of the file that declared block:
// relative paths in a block resolve against it, so split configs behave the
// way they read.
func blockBaseDir(cfg *Config, block *hcl.Block) string {
	baseDir := "."
	if f := block.DefRange.Filename; f != "" {
		baseDir = filepath.Dir(f)
	} else if cfg.path != "" {
		baseDir = filepath.Dir(cfg.path)
	}
	if !filepath.IsAbs(baseDir) {
		if abs, err := filepath.Abs(baseDir); err == nil {
			baseDir = abs
		}
	}
	return baseDir
}

func resolveAgainst(baseDir, p string) string {
	if p == "" {
		return baseDir
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(baseDir, p)
}

// checkCycles walks every declared block's references and `after` edges —
// modules, apps, services and tasks alike — and reports a cycle. Instance-only
// cycles are caught earlier, by topoOrder; this adds the kinds that are not
// instances yet.
func (cfg *Config) checkCycles(parser *hclparse.Parser, ranges map[string]hcl.Range) error {
	edges := map[string][]string{}
	for _, d := range cfg.Instances {
		for _, dep := range d.Deps {
			edges[d.Name] = append(edges[d.Name], dep.Name)
		}
		for _, r := range append(append([]Ref{}, d.Refs...), d.After...) {
			edges[d.Name] = append(edges[d.Name], r.Name)
		}
	}
	for _, s := range cfg.Services {
		for _, r := range append(append([]Ref{}, s.Refs...), s.After...) {
			edges[s.Name] = append(edges[s.Name], r.Name)
		}
	}
	for _, t := range cfg.Tasks {
		for _, r := range append(append([]Ref{}, t.Refs...), t.After...) {
			edges[t.Name] = append(edges[t.Name], r.Name)
		}
	}
	names := make([]string, 0, len(edges))
	for n := range edges {
		names = append(names, n)
	}
	sort.Strings(names)

	const (
		white = iota
		gray
		black
	)
	state := map[string]int{}
	var visit func(n string, stack []string) error
	visit = func(n string, stack []string) error {
		switch state[n] {
		case black:
			return nil
		case gray:
			cycle := append(append([]string{}, stack...), n)
			return posErr(parser, ranges[n], "dependency cycle", strings.Join(cycle, " → "))
		}
		state[n] = gray
		for _, m := range edges[n] {
			if err := visit(m, append(stack, n)); err != nil {
				return err
			}
		}
		state[n] = black
		return nil
	}
	for _, n := range names {
		if err := visit(n, nil); err != nil {
			return err
		}
	}
	return nil
}
