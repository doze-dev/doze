package daemon

import (
	"context"
	"strings"
	"time"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/control"
)

// consoleGap is how often a console's tables are read while someone is
// looking. A console costs nothing the rest of the time: nothing here runs
// without a subscription.
const consoleGap = 2 * time.Second

// subConsole serves one instance's console to a subscriber: its spec and
// actions first, then its tables on an interval and, if the subscriber named a
// stream panel, that panel's rows as they arrive.
//
// It never wakes the instance. A sleeping module has a spec and no rows, and
// the console says so; the rows start when something else wakes it.
func (h *handler) subConsole(ctx context.Context, names []string, panel, selector string, send func(control.Frame) error) {
	if len(names) != 1 {
		_ = send(control.Frame{Topic: control.TopicConsole, Console: &control.ConsoleFrame{Error: "the console topic follows exactly one instance"}})
		return
	}
	name := names[0]
	emit := func(f control.ConsoleFrame) bool {
		f.Instance = name
		return send(control.Frame{Topic: control.TopicConsole, Console: &f}) == nil
	}

	adm, inst, err := h.d.rt.AdminFor(name)
	if err != nil {
		emit(control.ConsoleFrame{Error: err.Error()})
		return
	}
	console, _ := adm.(engine.Console)
	var spec engine.ConsoleSpec
	if console != nil {
		spec = console.DescribeConsole()
	}
	first := control.ConsoleFrame{Spec: specView(spec)}
	if adm != nil {
		for _, a := range adm.Actions() {
			first.Actions = append(first.Actions, control.ActionView{ID: a.ID, Label: a.Label, Kind: a.Kind, Destructive: a.Destructive, InputHint: a.InputHint})
		}
	}
	first.State = h.stateOf(name)
	if !emit(first) {
		return
	}
	if adm == nil || len(spec.Panels) == 0 {
		<-ctx.Done()
		return
	}

	// The stream, if one was asked for: it runs for as long as the
	// subscription and stops with it.
	if panel != "" && console != nil && h.d.rt.Backend(name) != nil {
		rows := make(chan engine.Row, 64)
		go func() {
			defer close(rows)
			if err := console.Tail(ctx, inst, inst.Endpoint, panel, selector, rows); err != nil && ctx.Err() == nil {
				emit(control.ConsoleFrame{Panel: panel, Error: err.Error()})
			}
		}()
		go func() {
			for r := range rows {
				row := control.ConsoleRow{Cells: r.Cells, Tone: r.Tone}
				if !emit(control.ConsoleFrame{Panel: panel, Row: &row}) {
					return
				}
			}
		}()
	}

	ticker := time.NewTicker(consoleGap)
	defer ticker.Stop()
	for {
		state := h.stateOf(name)
		if h.d.rt.Backend(name) != nil {
			res, err := adm.Resources(ctx, inst, inst.Endpoint)
			if err != nil {
				if !emit(control.ConsoleFrame{State: state, Error: err.Error()}) {
					return
				}
			} else {
				declared := declaredObjects(adm, inst)
				for _, p := range spec.Panels {
					if p.Kind != "table" && p.Kind != "counters" {
						continue
					}
					if !emit(control.ConsoleFrame{State: state, Panel: p.ID, Rows: tableRows(p, res, declared)}) {
						return
					}
				}
			}
		} else if !emit(control.ConsoleFrame{State: state}) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// tableRows turns the resources of a panel's kind into rows keyed by column.
func tableRows(p engine.Panel, res []engine.Resource, declared map[string]map[string]bool) []control.ConsoleRow {
	var out []control.ConsoleRow
	for _, r := range res {
		if r.Kind != p.Source {
			continue
		}
		cells := map[string]string{"name": r.Name, "status": r.Status}
		for k, v := range r.Info {
			cells[k] = v
		}
		// A resource that exists but the config does not declare was made while
		// the stack ran. Only said of a kind the config does declare some of:
		// without that, there is nothing to tell the two apart by.
		if names, ok := declared[r.Kind]; ok && !names[r.Name] {
			cells[control.CellRuntime] = "1"
		}
		out = append(out, control.ConsoleRow{Cells: cells})
	}
	return out
}

func specView(s engine.ConsoleSpec) *control.ConsoleSpec {
	out := &control.ConsoleSpec{Front: s.Front, WebURL: s.WebURL}
	for _, p := range s.Panels {
		pv := control.ConsolePanel{ID: p.ID, Title: p.Title, Kind: p.Kind, Source: p.Source, Select: p.Select}
		for _, c := range p.Columns {
			pv.Columns = append(pv.Columns, control.ConsoleColumn{Key: c.Key, Title: c.Title, Align: c.Align, Role: c.Role})
		}
		out.Panels = append(out.Panels, pv)
	}
	for _, t := range s.Tabs {
		out.Tabs = append(out.Tabs, control.ConsoleTab{ID: t.ID, Title: t.Title, Body: nodeView(t.Body)})
	}
	for _, e := range s.Extend {
		out.Extend = append(out.Extend, control.ConsoleExtension{After: e.After, Node: nodeView(e.Node)})
	}
	return out
}

func nodeView(n engine.Node) control.ConsoleNode {
	out := control.ConsoleNode{Kind: n.Kind, ID: n.ID, Title: n.Title, Weight: n.Weight, Panel: n.Panel, From: n.From, Text: n.Text, Actions: n.Actions}
	for _, c := range n.Children {
		out.Children = append(out.Children, nodeView(c))
	}
	return out
}

// stateOf is an instance's daemon state word, "" when it is not known.
func (h *handler) stateOf(name string) string {
	if inst, ok := h.d.rt.Registry().Get(name); ok {
		return strings.TrimSpace(string(inst.State))
	}
	return ""
}

// declaredObjects is what the config declares for an instance, by kind and
// name, or nil when the module cannot say.
func declaredObjects(adm engine.Admin, inst engine.Instance) map[string]map[string]bool {
	inv, ok := adm.(engine.Inventory)
	if !ok {
		return nil
	}
	out := map[string]map[string]bool{}
	for _, o := range inv.Objects(inst) {
		if out[o.Kind] == nil {
			out[o.Kind] = map[string]bool{}
		}
		out[o.Kind][o.Name] = true
	}
	return out
}
