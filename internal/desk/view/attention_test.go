package view

import (
	"strings"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/journal"
)

var t0 = time.Date(2026, 11, 6, 14, 0, 0, 0, time.UTC)

func running(insts ...control.InstanceView) hub.StackView {
	return hub.StackView{Key: "/h/projects/shop-1", Name: "shop", Dir: "/code/shop", Running: true, Conn: hub.ConnLive, Instances: insts}
}

func TestStackItemsCrashNamesThePortHolder(t *testing.T) {
	st := running(control.InstanceView{Name: "api", Kind: "app", State: "crashing", LastError: "listen tcp 127.0.0.1:8080: bind: address already in use", RestartCount: 3, RestartMax: 5})
	f := StackFacts{
		Port: func(control.InstanceView) int { return 8080 },
		Held: func(int) *PortHolder { return &PortHolder{Port: 8080, PID: 4121, Command: "node"} },
	}
	items := StackItems(st, f, t0)
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	it := items[0]
	if !it.Red || it.Title != "api keeps crashing" || it.Detail != "port 8080 is held by node (pid 4121) · try 3 of 5" {
		t.Errorf("item = %+v", it)
	}
	if it.Fixes[0].Kind != "holder" || it.Fixes[0].PID != 4121 || it.Fixes[0].Label != "Stop pid 4121 and start api" {
		t.Errorf("first fix = %+v", it.Fixes[0])
	}
	// The try count moves; what it says for Not now does not.
	st.Instances[0].RestartCount = 4
	if again := StackItems(st, f, t0); again[0].Print != it.Print {
		t.Errorf("print changed with the try: %q → %q", it.Print, again[0].Print)
	}
}

func TestStackItemsTaskFailedAndStack(t *testing.T) {
	st := running(
		control.InstanceView{Name: "migrate", Kind: "task", Task: &control.TaskView{Result: "failed", ExitCode: 1, RanAt: t0}, LastError: "relation \"orders\" does not exist"},
		control.InstanceView{Name: "db", Kind: "module", State: "running"},
	)
	items := StackItems(st, StackFacts{Changed: &ConfigChanged{Text: "doze.hcl changed 4m ago"}}, t0)
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Kind != "task" || items[0].Title != "migrate failed" || items[0].Fixes[0].Verb != "task" {
		t.Errorf("task item = %+v", items[0])
	}
	if items[1].Kind != "changed" || items[1].Red || items[1].Fixes[0].Verb != "restart" {
		t.Errorf("changed item = %+v", items[1])
	}
}

func TestStackItemsMissingSaysOnlyThat(t *testing.T) {
	st := hub.StackView{Key: "k", Name: "playground", Dir: "/tmp/playground"}
	items := StackItems(st, StackFacts{Missing: "the folder is gone", Config: &ConfigError{Summary: "x"}}, t0)
	if len(items) != 1 || items[0].Title != "playground's folder is gone" || items[0].Fixes[0].Verb != "remove" {
		t.Errorf("items = %+v", items)
	}
}

func TestStackItemsDaemonDownAndFailedStart(t *testing.T) {
	st := running()
	st.Conn, st.LastFrame = hub.ConnDown, t0.Add(-2*time.Minute)
	items := StackItems(st, StackFacts{Failed: &StackFailure{What: "Restarting shop failed", Lines: []string{"exit 1"}}}, t0)
	if len(items) != 2 || items[0].Kind != "failed" || items[1].Kind != "daemon" || !items[1].Red {
		t.Fatalf("items = %+v", items)
	}
	// A running stack's failure cannot be tried again from here: only dismissed.
	if len(items[0].Fixes) != 1 || items[0].Fixes[0].Kind != "dismiss" {
		t.Errorf("failed fixes = %+v", items[0].Fixes)
	}
}

func TestAttentionOfSortsCountsAndHides(t *testing.T) {
	items := []Item{
		{ID: "y1", Print: "a", Title: "yellow"},
		{ID: "r1", Print: "b", Red: true, Title: "red"},
		{ID: "r2", Print: "c", Red: true, Title: "hidden"},
		{ID: "g", Print: "d", Red: true, Fixed: true, Title: "fixed"},
	}
	a := AttentionOf(items, map[string]string{"r2": "c", "y1": "old"})
	if a.Red != 1 || a.Yellow != 1 || a.Hidden != 1 {
		t.Errorf("counts = %d red, %d yellow, %d hidden", a.Red, a.Yellow, a.Hidden)
	}
	var order []string
	for _, it := range a.Items {
		order = append(order, it.ID)
	}
	if strings.Join(order, ",") != "r1,y1,g" {
		t.Errorf("order = %v", order)
	}
}

func TestMachineItems(t *testing.T) {
	checks := []Check{{Section: "names", Name: "resolver route", OK: true}, {Section: "names", Name: "network", Fix: "doze dns-setup"}, {Section: "domains", Name: "route dev.acme.com", Fix: "doze trust"}}
	items := MachineItems(checks, false, 3, 0)
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Fixes[0].Kind != "copy" || items[0].Fixes[0].Text != "doze dns-setup" {
		t.Errorf("names fix = %+v", items[0].Fixes)
	}
	if items[2].Title != "3 things left behind" || items[2].Fixes[0].Kind != "clean" {
		t.Errorf("leftovers = %+v", items[2])
	}
	if got := MachineItems(checks, true, 0, 2); got[0].Fixes[0].Kind != "repair" || got[len(got)-1].ID != "leftdata" {
		t.Errorf("repairable fix = %+v", got[0].Fixes)
	}
}

func TestAwayOf(t *testing.T) {
	closed := t0.Add(-3*time.Hour - 12*time.Minute)
	st := running(
		control.InstanceView{Name: "api", Kind: "app", State: "running"},
		control.InstanceView{Name: "migrate", Kind: "task", Task: &control.TaskView{Result: "failed", ExitCode: 1, RanAt: t0.Add(-20 * time.Minute)}},
	)
	st.Events = []journal.Event{
		{Time: closed.Add(-time.Minute), Kind: journal.Crash, Instance: "api"}, // before: not news
		{Time: t0.Add(-90 * time.Minute), Kind: journal.Crash, Instance: "api"},
		{Time: t0.Add(-80 * time.Minute), Kind: journal.Crash, Instance: "api"},
		{Time: t0.Add(-70 * time.Minute), Kind: journal.Wake, Instance: "api"},
	}
	a := AwayOf([]hub.StackView{st}, closed, t0)
	if a == nil || a.For != "3 h 12 m" || len(a.Events) != 2 {
		t.Fatalf("away = %+v", a)
	}
	if a.Events[0].Instance != "migrate" || a.Events[0].Tone != "bad" {
		t.Errorf("newest = %+v", a.Events[0])
	}
	if a.Events[1].What != "crashed twice in shop and started again" {
		t.Errorf("api = %+v", a.Events[1])
	}
	if AwayOf([]hub.StackView{running()}, closed, t0) != nil {
		t.Error("nothing happened, yet a strip")
	}
}

func TestStuckWaking(t *testing.T) {
	st := running(control.InstanceView{Name: "kafka", Kind: "module", State: "booting"})
	st.Events = []journal.Event{{Time: t0.Add(-6 * time.Minute), Kind: journal.Wake, Instance: "kafka"}, {Time: t0.Add(-3 * time.Minute), Kind: journal.Step, Instance: "kafka"}}
	items := StackItems(st, StackFacts{}, t0)
	if len(items) != 1 || items[0].Title != "kafka is still waking" || !items[0].Red {
		t.Fatalf("items = %+v", items)
	}
	// A step a moment ago: it is still saying things, so not stuck.
	st.Events = append(st.Events, journal.Event{Time: t0.Add(-10 * time.Second), Kind: journal.Step, Instance: "kafka"})
	if items := StackItems(st, StackFacts{}, t0); len(items) != 0 {
		t.Errorf("busy wake reported stuck: %+v", items)
	}
}
