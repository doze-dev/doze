package daemon

import (
	"context"
	"testing"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
)

func TestTableRowsMarkWhatWasMadeAtRuntime(t *testing.T) {
	panel := engine.Panel{ID: "topics", Kind: "table", Source: "topic"}
	res := []engine.Resource{
		{Kind: "topic", Name: "orders", Status: "3 partitions", Info: map[string]string{"high": "10"}},
		{Kind: "topic", Name: "scratch", Status: "1 partition"},
		{Kind: "group", Name: "billing"},
	}

	// The config declares orders. scratch exists and is not declared: runtime.
	rows := tableRows(panel, res, map[string]map[string]bool{"topic": {"orders": true}})
	if len(rows) != 2 {
		t.Fatalf("rows of the panel's own kind only: %+v", rows)
	}
	if rows[0].Cells[control.CellRuntime] != "" || rows[0].Cells["high"] != "10" || rows[0].Cells["name"] != "orders" {
		t.Errorf("a declared resource is not marked, and carries its info: %+v", rows[0])
	}
	if rows[1].Cells[control.CellRuntime] == "" {
		t.Errorf("an undeclared one is: %+v", rows[1])
	}

	// A config that declares no topics at all gives nothing to tell them apart by.
	for _, r := range tableRows(panel, res, map[string]map[string]bool{"queue": {"jobs": true}}) {
		if r.Cells[control.CellRuntime] != "" {
			t.Errorf("no evidence, no marker: %+v", r)
		}
	}
	// And a module that cannot say what is declared marks nothing.
	for _, r := range tableRows(panel, res, nil) {
		if r.Cells[control.CellRuntime] != "" {
			t.Errorf("no inventory, no marker: %+v", r)
		}
	}
}

// countingModule is an admin module with a console that counts what it is asked.
type countingModule struct {
	engine.Driver
	describes, resources, tails int
}

func (m *countingModule) Type() string             { return "countingconsole" }
func (m *countingModule) Actions() []engine.Action { return nil }
func (m *countingModule) Resources(context.Context, engine.Instance, engine.Endpoint) ([]engine.Resource, error) {
	m.resources++
	return nil, nil
}
func (m *countingModule) Run(context.Context, engine.Instance, engine.Endpoint, string, string, string) (string, error) {
	return "", nil
}
func (m *countingModule) DescribeConsole() engine.ConsoleSpec {
	m.describes++
	return engine.ConsoleSpec{Front: "console", Panels: []engine.Panel{{ID: "p", Kind: "table", Source: "x"}}}
}
func (m *countingModule) Tail(context.Context, engine.Instance, engine.Endpoint, string, string, chan<- engine.Row) error {
	m.tails++
	return nil
}

// consoles.md measure: a console costs nothing when it is closed. Everything
// the daemon does about consoles with nobody looking is say that one exists —
// on every snapshot — and that must not read a table or open a stream.
func TestAConsoleCostsNothingWhileNobodyLooks(t *testing.T) {
	m := &countingModule{}
	engine.Register(m)
	h := &handler{}
	decl := &config.InstanceDecl{Name: "thing", Type: "countingconsole"}

	for i := 0; i < 200; i++ { // 200 snapshots, a few minutes of a dash on home
		var v control.InstanceView
		h.hydrateConsole(&v, decl)
		if !v.HasConsole || v.ConsoleFront != "console" {
			t.Fatalf("the instance should say it has a console: %+v", v)
		}
	}
	if m.resources != 0 || m.tails != 0 {
		t.Fatalf("with nobody looking the module was asked for %d table reads and %d tails", m.resources, m.tails)
	}
}
