package view

import (
	"strings"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/journal"
)

// A day of a stack: db slept most of it and was woken by api and by a
// connection; api crashed once; the stack was down for two hours, which is
// not counted as sleep.
func TestActivityCountsSleepWhileUpAndWhatWoke(t *testing.T) {
	now := time.Date(2026, 11, 6, 20, 0, 0, 0, time.UTC)
	at := func(hAgo float64) time.Time { return now.Add(-time.Duration(hAgo * float64(time.Hour))) }
	st := hub.StackView{Key: "k", Name: "shop", Running: true, Instances: []control.InstanceView{
		{Name: "api", Kind: "app", State: "active"},
		{Name: "db", Kind: "module", State: "reaped"},
	}}
	events := []journal.Event{
		{Time: at(30), Kind: journal.Wake, Instance: "api", Cause: journal.Cause{Kind: journal.ByCommand}}, // before the day
		{Time: at(20), Kind: journal.Wake, Instance: "db", Cause: journal.Cause{Kind: journal.ByDependency, By: "api"}},
		{Time: at(19), Kind: journal.Sleep, Instance: "db", Cause: journal.Cause{Kind: journal.ByIdle}},
		{Time: at(12), Kind: journal.Sleep, Instance: "api", Cause: journal.Cause{Kind: journal.ByShutdown}},
		{Time: at(12), Kind: journal.Sleep, Instance: "db", Cause: journal.Cause{Kind: journal.ByShutdown}},
		{Time: at(10), Kind: journal.Wake, Instance: "api", Cause: journal.Cause{Kind: journal.ByCommand}},
		{Time: at(5), Kind: journal.Wake, Instance: "db", Cause: journal.Cause{Kind: journal.ByConnection}},
		{Time: at(4), Kind: journal.Sleep, Instance: "db", Cause: journal.Cause{Kind: journal.ByIdle}},
		{Time: at(2), Kind: journal.Crash, Instance: "api", Detail: "panic: boom"},
		{Time: at(1.9), Kind: journal.Wake, Instance: "api", Cause: journal.Cause{Kind: journal.ByRestart}},
	}
	a := ActivityOf(st, events, "24h", "", map[string]int64{"db": 512 << 20}, now)
	if a.Summary.Up != "22h of 24h" {
		t.Errorf("up = %q", a.Summary.Up)
	}
	var db, api ActivityRow
	for _, r := range a.Rows {
		switch r.Name {
		case "db":
			db = r
		case "api":
			api = r
		}
	}
	// db: up 22h, awake 2h of it, asleep 20h.
	if db.Asleep != "20h asleep · 91%" || db.Wakes != 2 || db.WokenBy != "1 by a connection · 1 for api" {
		t.Errorf("db = %+v", db)
	}
	if db.Freed != "~10.0 GB·h" || !strings.HasPrefix(a.Summary.Freed, "about 10.0 GB·h") {
		t.Errorf("freed = %q, %q", db.Freed, a.Summary.Freed)
	}
	if api.Crashes != 1 || len(a.Trouble) != 1 || !strings.Contains(a.Trouble[0].Text, "api") {
		t.Errorf("api = %+v, trouble = %+v", api, a.Trouble)
	}
	if a.Events[0].Text == "" || a.Short != "" {
		t.Errorf("events = %+v, short = %q", a.Events[:1], a.Short)
	}
	// One service alone; and a week, which the journal does not reach.
	if one := ActivityOf(st, events, "24h", "db", nil, now); len(one.Rows) != 1 || one.Rows[0].Name != "db" {
		t.Errorf("db alone = %+v", one.Rows)
	}
	if week := ActivityOf(st, events, "7d", "", nil, now); !strings.HasPrefix(week.Short, "the journal goes back") {
		t.Errorf("week short = %q", week.Short)
	}
}

// A service awake since before the journal begins is awake, not asleep.
func TestActivityAwakeBeforeTheJournal(t *testing.T) {
	now := time.Date(2026, 11, 6, 20, 0, 0, 0, time.UTC)
	st := hub.StackView{Key: "k", Name: "shop", Running: true, Instances: []control.InstanceView{
		{Name: "api", Kind: "app", State: "active"},
		{Name: "db", Kind: "module", State: "reaped"},
	}}
	events := []journal.Event{
		{Time: now.Add(-30 * time.Hour), Kind: journal.Wake, Instance: "db", Cause: journal.Cause{Kind: journal.ByCommand}},
		{Time: now.Add(-29 * time.Hour), Kind: journal.Sleep, Instance: "db", Cause: journal.Cause{Kind: journal.ByIdle}},
	}
	a := ActivityOf(st, events, "24h", "", nil, now)
	for _, r := range a.Rows {
		if r.Name == "api" && (r.AsleepPart != 0 || !r.Never) {
			t.Errorf("api, awake all along = %+v", r)
		}
		if r.Name == "db" && r.AsleepPart != 1 {
			t.Errorf("db, asleep all day = %+v", r)
		}
	}
}
