// handler adapts the Daemon's runtime to the control.Handler interface — the
// ~15 RPC operations the CLI (and in-process embedders) drive it with.
package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/endpoints"
	"github.com/doze-dev/doze/internal/registry"
	"github.com/doze-dev/doze/internal/ui"
)

// handler adapts the runtime to the control.Handler interface.
type handler struct{ d *Daemon }

func (h *handler) Status() control.Response {
	resp := control.Response{Listen: h.d.cfg.Listen, IdleTimeout: h.d.cfg.Defaults.IdleTimeout}
	eps := h.endpointsByName()
	snapshot := h.d.rt.Registry().Snapshot()
	pids := make([]int, 0, len(snapshot))
	for _, inst := range snapshot {
		if inst.PID != 0 {
			pids = append(pids, inst.PID)
		}
	}
	stats := ui.ProcStats(pids) // one ps for every running backend (+ its subtree)
	tree := ui.ProcTree(pids)  // …and each backend's own child processes
	seen := map[string]bool{}
	for _, inst := range snapshot {
		engineType, version, declared := "", "", false
		if decl := h.d.cfg.Lookup(inst.Name); decl != nil {
			engineType, version, declared = decl.Type, decl.Version.String(), true
		}
		v := control.ViewFromRegistry(inst, engineType, version, declared)
		if decl := h.d.cfg.Lookup(inst.Name); decl != nil && !decl.Enabled {
			v.Disabled = true
		}
		h.hydrateEndpoint(&v, eps[inst.Name])
		v.DataDir = h.dataDir(inst.Name)
		if st, ok := stats[inst.PID]; ok {
			v.RAM, v.CPU = st.RSS, st.CPU
		}
		if engineType == "process" {
			for _, n := range tree[inst.PID] {
				v.Children = append(v.Children, control.ProcView{
					PID: n.PID, RSS: n.RSS, CPU: n.CPU, Cmd: n.Cmd, Depth: n.Depth,
				})
			}
		}
		resp.Instances = append(resp.Instances, v)
		seen[inst.Name] = true
	}
	for _, decl := range h.d.cfg.Instances {
		if !seen[decl.Name] {
			state := "reaped"
			if !decl.Enabled {
				state = "disabled"
			}
			v := control.InstanceView{
				Name: decl.Name, Engine: decl.Type, State: state,
				Version: decl.Version.String(), Declared: true, Disabled: !decl.Enabled,
			}
			h.hydrateEndpoint(&v, eps[decl.Name])
			v.DataDir = h.dataDir(decl.Name)
			resp.Instances = append(resp.Instances, v)
		}
	}
	return resp
}

// dataDir is where an instance's backend writes its data.
func (h *handler) dataDir(name string) string {
	return filepath.Join(h.d.cfg.ClustersDir(), name)
}

func (h *handler) hydrateEndpoint(v *control.InstanceView, ep endpoints.Endpoint) {
	cfg := h.d.cfg
	// Show the address the instance actually listens on. In per-service mode this
	// is its own 127.0.0.x, so two Postgres on 5432 read as 127.0.0.11:5432 and
	// 127.0.0.12:5432 rather than both as the canonical 127.0.0.1:5432 (which is
	// only the declared port, not where anything binds). Falls back to ep.Address
	// for supervised processes and disabled instances (no proxy listener).
	v.Endpoint = ep.Address
	if bind := h.d.binds[v.Name]; bind != "" {
		v.Endpoint = bind
	}
	// Bind keeps the dialable truth even where Endpoint gets prettified below
	// (AWS built-ins swap in the shared host); surfaces show it as the raw
	// address behind the connect line.
	v.Bind = v.Endpoint
	v.Domain = ep.Domain
	v.URL = ep.URL
	v.EnvVar = ep.EnvVar
	// The aws engine is reached at the stack's AWS host; its backend port in
	// ep.Address is internal-only, so show the host instead.
	if shared, ok := cfg.AWSEndpoint(v.Engine); ok {
		v.Endpoint = strings.TrimPrefix(shared, "http://")
	}
	// The full, directly-addressable path (AWS resource URL/ARN, ingress :80 URL).
	v.Resource = h.d.resources[v.Name]
}

// endpointsByName maps instance name -> its resolved endpoint (best effort).
func (h *handler) endpointsByName() map[string]endpoints.Endpoint {
	out := map[string]endpoints.Endpoint{}
	eps, err := endpoints.For(h.d.cfg)
	if err != nil {
		return out
	}
	for _, ep := range eps {
		out[ep.Name] = ep
	}
	return out
}

func (h *handler) Boot(ctx context.Context, name string) error {
	if decl := h.d.cfg.Lookup(name); decl != nil && !decl.Enabled {
		return fmt.Errorf("instance %q is disabled (enabled = false); enable it in the config to wake it", name)
	}
	_, err := h.d.rt.Boot(ctx, name)
	return err
}

func (h *handler) Restart(ctx context.Context, name string) error {
	if err := h.d.rt.Stop(ctx, name); err != nil {
		return err
	}
	_, err := h.d.rt.Boot(ctx, name)
	return err
}

func (h *handler) Up(ctx context.Context, name string) error {
	if name == "" {
		for _, decl := range h.d.cfg.Instances {
			if !decl.Enabled {
				continue // paused: skip disabled instances on a whole-stack up
			}
			if err := h.d.rt.Up(ctx, decl.Name); err != nil {
				return err
			}
		}
		return nil
	}
	return h.d.rt.Up(ctx, name)
}

func (h *handler) Down(ctx context.Context, name string) error {
	if name == "" {
		h.d.rt.StopAll(ctx)
		return nil
	}
	return h.d.rt.Stop(ctx, name)
}

func (h *handler) Apply(ctx context.Context, name string) error {
	return h.d.rt.Apply(ctx, name)
}

func (h *handler) Destroy(ctx context.Context, name string) error {
	return h.d.rt.Destroy(ctx, name)
}

func (h *handler) Reset(ctx context.Context, name string) error {
	return h.d.rt.ResetData(ctx, name)
}

func (h *handler) KeepAwake(name string) error {
	if name == "" {
		return fmt.Errorf("keepawake needs an instance name")
	}
	h.d.rt.ToggleKeepAwake(name)
	return nil
}

func (h *handler) Logs(name string) ([]string, error) {
	p := h.d.rt.Backend(name)
	if p == nil {
		return nil, fmt.Errorf("instance %q is not running", name)
	}
	return p.Logs(), nil
}

// StreamLogs polls the named backends' log rings (every 250ms) and emits each new
// line, tagged with its instance, until ctx is cancelled or emit fails. Empty names
// follows every declared instance. A process restart resets its ring; the cursor
// regression is detected and the new process's output is streamed from the start.
func (h *handler) StreamLogs(ctx context.Context, names []string, emit func(control.LogFrame) error) error {
	if len(names) == 0 {
		for _, d := range h.d.cfg.Instances {
			names = append(names, d.Name)
		}
	}
	sent := map[string]int{}
	last := map[string]engine.Process{} // last backend seen per name, to detect a restart by identity
	flush := func() error {
		for _, n := range names {
			p := h.d.rt.Backend(n)
			if p == nil {
				continue
			}
			if last[n] != p { // first sighting, or a restart replaced the ring — stream from its start
				sent[n] = 0
				last[n] = p
			}
			ls, ok := p.(interface {
				LogsSince(int) ([]string, int)
			})
			if !ok {
				continue
			}
			lines, cursor := ls.LogsSince(sent[n])
			for _, line := range lines {
				if err := emit(control.LogFrame{Instance: n, Line: line}); err != nil {
					return err
				}
			}
			sent[n] = cursor
		}
		return nil
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	if err := flush(); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := flush(); err != nil {
				return err
			}
		}
	}
}

// StreamEvents forwards instance-state transitions to emit until ctx ends or the
// client disconnects. It subscribes to the registry's lossy feed, enriches each
// transition with config/endpoint metadata (the same shape Status returns, minus
// the per-instance ps stats), and emits it.
func (h *handler) StreamEvents(ctx context.Context, emit func(control.EventFrame) error) error {
	feed, cancel := h.d.rt.Registry().Subscribe(64)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case inst, ok := <-feed:
			if !ok {
				return nil
			}
			if err := emit(control.EventFrame{Instance: h.eventView(inst)}); err != nil {
				return err
			}
		}
	}
}

// AddInstance wires a rendered HCL block into the running stack and returns the
// new instance's view.
func (h *handler) AddInstance(ctx context.Context, block string) (control.InstanceView, error) {
	name, err := h.d.AddInstance(ctx, block)
	if err != nil {
		return control.InstanceView{}, err
	}
	for _, v := range h.Status().Instances {
		if v.Name == name {
			return v, nil
		}
	}
	return control.InstanceView{Name: name, Declared: true}, nil
}

// RemoveInstance tears an instance down.
func (h *handler) RemoveInstance(ctx context.Context, name string, wipe bool) error {
	return h.d.RemoveInstance(ctx, name, wipe)
}

// eventView enriches a registry snapshot into a full InstanceView for the events
// stream, mirroring Status's per-instance enrichment without the ps call.
func (h *handler) eventView(inst registry.Instance) control.InstanceView {
	engineType, version, declared := "", "", false
	if decl := h.d.cfg.Lookup(inst.Name); decl != nil {
		engineType, version, declared = decl.Type, decl.Version.String(), true
	}
	v := control.ViewFromRegistry(inst, engineType, version, declared)
	if decl := h.d.cfg.Lookup(inst.Name); decl != nil && !decl.Enabled {
		v.Disabled = true
	}
	h.hydrateEndpoint(&v, h.endpointsByName()[inst.Name])
	v.DataDir = h.dataDir(inst.Name)
	return v
}

// Resources lists a builtin instance's sub-resources (queues/buckets/topics) with
// a live status line, plus the data actions its engine offers. Empty (no error)
// when the engine has no admin capability; an error when it isn't running.
func (h *handler) Resources(ctx context.Context, name string) ([]control.ResourceView, []control.ActionView, error) {
	adm, inst, err := h.d.rt.AdminFor(name)
	if err != nil {
		return nil, nil, err
	}
	if adm == nil {
		return nil, nil, nil
	}
	if h.d.rt.Backend(name) == nil {
		return nil, nil, fmt.Errorf("instance %q is not running — boot it first", name)
	}
	res, err := adm.Resources(ctx, inst, inst.Endpoint)
	if err != nil {
		return nil, nil, err
	}
	rv := make([]control.ResourceView, 0, len(res))
	for _, r := range res {
		rv = append(rv, control.ResourceView{Kind: r.Kind, Name: r.Name, Status: r.Status, Info: r.Info})
	}
	acts := adm.Actions()
	av := make([]control.ActionView, 0, len(acts))
	for _, a := range acts {
		av = append(av, control.ActionView{
			ID: a.ID, Label: a.Label, Kind: a.Kind, Destructive: a.Destructive, InputHint: a.InputHint,
		})
	}
	return rv, av, nil
}

// Admin runs a builtin data action (purge/empty/publish/…) on a named resource.
func (h *handler) Admin(ctx context.Context, name, action, resource, input string) (string, error) {
	adm, inst, err := h.d.rt.AdminFor(name)
	if err != nil {
		return "", err
	}
	if adm == nil {
		return "", fmt.Errorf("instance %q (%s) has no data actions", name, inst.Type)
	}
	if h.d.rt.Backend(name) == nil {
		return "", fmt.Errorf("instance %q is not running — boot it first", name)
	}
	return adm.Run(ctx, inst, inst.Endpoint, action, resource, input)
}
