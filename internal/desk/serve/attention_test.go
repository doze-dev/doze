package serve

import (
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fixture"
	"github.com/doze-dev/doze/internal/desk/view"
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
