package tui

import (
	"testing"

	"github.com/doze-dev/doze/internal/control"
)

// The dash's rows are positions in a slice, so frames must update in place:
// a row that wakes keeps its place and its meters, and a meter sample never
// outlives the process it measured.
func TestPumpStateFoldsFrames(t *testing.T) {
	s := &pumpState{resp: control.Response{Instances: []control.InstanceView{
		{Name: "db", State: "active", PID: 10, RAM: 11, CPU: 1},
		{Name: "api", State: "reaped"},
	}}}

	// api wakes: same row, and db's meters are untouched.
	s.instances(control.Frame{Instances: []control.InstanceView{{Name: "api", State: "active", PID: 20}}})
	if got := s.resp.Instances; got[1].Name != "api" || got[1].PID != 20 || got[0].RAM != 11 {
		t.Fatalf("after a wake: %+v", got)
	}
	// db changes without restarting (a pin): its meters carry over.
	s.instances(control.Frame{Instances: []control.InstanceView{{Name: "db", State: "active", PID: 10, KeepAwake: true}}})
	if db := s.resp.Instances[0]; !db.KeepAwake || db.RAM != 11 {
		t.Fatalf("a change to a running instance lost its meters: %+v", db)
	}
	// db restarts: a new process starts with no meters, not the old one's.
	s.instances(control.Frame{Instances: []control.InstanceView{{Name: "db", State: "active", PID: 11}}})
	if db := s.resp.Instances[0]; db.RAM != 0 {
		t.Fatalf("a restarted instance kept the old process's meters: %+v", db)
	}

	// A sample fills in what is running and clears what is not.
	s.stats(control.Frame{Stats: []control.StatView{
		{Name: "db", PID: 11, RAM: 12, CPU: 2},
		{Name: "api", PID: 20, RAM: 40, Children: []control.ProcView{{PID: 21}}},
	}})
	if db, api := s.resp.Instances[0], s.resp.Instances[1]; db.RAM != 12 || api.RAM != 40 || len(api.Children) != 1 {
		t.Fatalf("after a sample: %+v", s.resp.Instances)
	}
	s.instances(control.Frame{Instances: []control.InstanceView{{Name: "api", State: "reaped"}}})
	s.stats(control.Frame{Stats: []control.StatView{{Name: "db", PID: 11, RAM: 13}}})
	if api := s.resp.Instances[1]; api.RAM != 0 || api.Children != nil {
		t.Fatalf("a stopped instance still shows meters: %+v", api)
	}

	// A new instance goes at the end; a removed one goes.
	s.instances(control.Frame{Instances: []control.InstanceView{{Name: "cache"}}, Removed: []string{"api"}})
	if got := s.resp.Instances; len(got) != 2 || got[0].Name != "db" || got[1].Name != "cache" {
		t.Fatalf("after an add and a remove: %+v", got)
	}

	// What the model is handed is its own copy.
	msg := s.msg(true)
	msg.resp.Instances[0].Name = "scribbled"
	if s.resp.Instances[0].Name != "db" {
		t.Fatal("the message shares the pump's slice")
	}
}
