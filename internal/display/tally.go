package display

import "github.com/doze-dev/doze/internal/control"

// Tally is a stack's services counted by the word each one shows, tasks
// aside, with the memory and client connections of the ones serving. Every
// surface that counts (the overview's strip, the Library row, the menu
// bar) counts with this, so the same stack never reads "4 awake" in one
// place and "7 awake" in another.
type Tally struct {
	Awake, Dozing, Waking, Asleep, Off, Paused, Trouble int
	// Failed counts tasks whose last run failed; they are trouble too.
	Failed int
	RAM    int64
	Conns  int
}

// TallyOf counts instances.
func TallyOf(vs []control.InstanceView) Tally {
	var t Tally
	for _, v := range vs {
		if v.Kind == "task" {
			if v.Task != nil && v.Task.Result == "failed" {
				t.Failed++
			}
			continue
		}
		switch LookOf(v).Word {
		case "awake":
			t.Awake++
		case "dozing":
			t.Dozing++
		case "waking":
			t.Waking++
		case "off":
			t.Off++
		case "paused":
			t.Paused++
		case "crashing", "failed":
			t.Trouble++
		default:
			t.Asleep++
		}
		if AwakeNow(v) {
			t.RAM += v.RAM
			t.Conns += Connections(v)
		}
	}
	return t
}
