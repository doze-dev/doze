package tui

import (
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/doze-dev/doze/internal/control"
)

// Two admin snapshots a second apart must accumulate history and yield a rate.
func TestKafkaHistoryAccumulates(t *testing.T) {
	fi := textinput.New()
	fi.Prompt = "/"
	m := model{
		width: 110, height: 30,
		filter: fi,
		hist:   map[string]*history{},
		logVP:  viewport.New(40, 8),
		resp:   control.Response{Instances: []control.InstanceView{{Name: "events", Engine: "kafka", PID: 1}}},
	}

	send := func(high string) {
		res := []control.ResourceView{{Kind: "topic", Name: "pageviews", Info: map[string]string{
			"partitions": "1", "high": high, "parts": high,
		}}}
		mm, _ := m.Update(resourcesMsg{name: "events", res: res})
		m = mm.(model)
	}
	send("10")
	time.Sleep(20 * time.Millisecond)
	send("32")

	s := m.khist["t:events:pageviews"]
	if len(s) != 2 {
		t.Fatalf("khist has %d samples, want 2 (khist=%v)", len(s), m.khist)
	}
	perMin, _ := krate(s, 8)
	if perMin != 22 {
		t.Fatalf("perMin = %d, want 22", perMin)
	}
}
