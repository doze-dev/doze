package serve

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fixture"
	"github.com/doze-dev/doze/internal/desk/view"
	"github.com/doze-dev/doze/internal/projects"
)

func itemFor(a view.Attention, id string) (view.Item, bool) {
	for _, it := range a.Items {
		if it.ID == id {
			return it, true
		}
	}
	return view.Item{}, false
}

// A crash that stops is shown fixed only after its grace, then leaves; one
// put aside with Not now leaves without a word.
func TestAttentionGraceThenFixed(t *testing.T) {
	for _, aside := range []bool{false, true} {
		f := fixture.Crash(epoch)
		h, key := newHub(t, f)
		s := New(h, nil)
		now := epoch
		s.SetClock(func() time.Time { return now })
		id := "instance:" + key + "/api"
		a := s.Attention()
		it, ok := itemFor(a, id)
		if !ok || !it.Red {
			t.Fatalf("no red api item: %+v", a.Items)
		}
		if aside {
			s.NotNow(id, it.Print)
		}
		f.Mutate(func(sn *control.Snapshot) {
			for i := range sn.Instances {
				if sn.Instances[i].Name == "api" {
					sn.Instances[i].State, sn.Instances[i].LastError = "running", ""
				}
			}
		})
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if st, _ := s.stack(key); len(st.Instances) > 0 && !troubledNamed(st.Instances, "api") {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		now = now.Add(time.Second)
		if it, ok := itemFor(s.Attention(), id); !aside && (!ok || it.Fixed) {
			t.Fatalf("within its grace the item should stay as it was: %+v %v", it, ok)
		}
		now = now.Add(graceFor + time.Second)
		it, ok = itemFor(s.Attention(), id)
		switch {
		case aside && ok:
			t.Errorf("put aside, yet shown: %+v", it)
		case !aside && (!ok || !it.Fixed):
			t.Errorf("after its grace it should say fixed: %+v %v", it, ok)
		}
		now = now.Add(fixedFor + time.Second)
		if _, ok := itemFor(s.Attention(), id); ok {
			t.Errorf("a fixed item should leave after %s", fixedFor)
		}
	}
}

func troubledNamed(vs []control.InstanceView, name string) bool {
	for _, v := range vs {
		if v.Name == name {
			return v.State == "crashing"
		}
	}
	return false
}

// A failed task stays red when its stack stops, and offers Up instead of
// Run again.
func TestFailedTaskOutlivesItsStack(t *testing.T) {
	f := fixture.Shop(epoch)
	f.Mutate(func(sn *control.Snapshot) {
		for i := range sn.Instances {
			if v := &sn.Instances[i]; v.Kind == "task" {
				v.Task = &control.TaskView{Result: "failed", ExitCode: 1, RanAt: epoch}
			}
		}
	})
	h, key := newHub(t, f)
	s := New(h, nil)
	now := epoch
	s.SetClock(func() time.Time { return now })
	var task view.Item
	for _, it := range s.Attention().Items {
		if it.Kind == "task" {
			task = it
		}
	}
	if task.ID == "" {
		t.Fatal("no failed task item")
	}
	// The stack stops: its record stays, its socket goes.
	cfgDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfgDir, "doze.hcl"), []byte("stack = \"shop\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := projects.Save(filepath.Join(h.Home(), "projects", "p-1"), projects.Project{Config: filepath.Join(cfgDir, "doze.hcl"), Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(h.Home(), "projects", "p-1", "run", "doze.admin.sock")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st, ok := s.stack(key); !ok || !st.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st, ok := s.stack(key); !ok || st.Running {
		t.Fatalf("the hub should see the stack stopped: %+v %v", st, ok)
	}
	now = now.Add(graceFor + time.Second)
	it, ok := itemFor(s.Attention(), task.ID)
	if !ok || it.Fixed || !it.Red || it.Fixes[0].Verb != "up-last" {
		t.Errorf("after its stack stopped: %+v %v", it, ok)
	}
}
