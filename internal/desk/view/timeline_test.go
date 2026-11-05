package view

import (
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/desk/hub"
	"github.com/doze-dev/doze/internal/journal"
)

func TestTimelineDrawsSpansAndCrashes(t *testing.T) {
	now := time.Date(2026, 11, 6, 2, 0, 0, 0, time.UTC)
	at := func(minAgo float64) time.Time { return now.Add(-time.Duration(minAgo * float64(time.Minute))) }
	st := hub.StackView{Key: "k", Name: "shop", Running: true,
		Instances: []control.InstanceView{
			{Name: "db", Kind: "module", State: "active"},
			{Name: "api", Kind: "app", State: "crashing"},
			{Name: "cache", Kind: "module", State: "reaped"},
		},
		Events: []journal.Event{
			{Time: at(45), Kind: journal.Wake, Instance: "db"}, // before the span: clipped
			{Time: at(20), Kind: journal.Sleep, Instance: "db", Cause: journal.Cause{Kind: journal.ByIdle}},
			{Time: at(15), Kind: journal.Wake, Instance: "db", Cause: journal.Cause{Kind: journal.ByDependency, By: "api"}},
			{Time: at(14.9), Kind: journal.Ready, Instance: "db"},
			{Time: at(15), Kind: journal.Wake, Instance: "api"},
			{Time: at(14), Kind: journal.Crash, Instance: "api", Detail: "panic: STRIPE_KEY is required"},
		},
	}
	tl := TimelineOf(st, now)
	if len(tl.Lanes) != 3 {
		t.Fatalf("lanes = %d, want every service", len(tl.Lanes))
	}
	lanes := map[string]TimelineLane{}
	for _, l := range tl.Lanes {
		lanes[l.Name] = l
	}
	db := lanes["db"]
	if len(db.Spans) != 2 || db.Spans[0].From != 0 || !db.Spans[1].Open || db.Spans[1].To != 1 {
		t.Fatalf("db spans = %+v", db.Spans)
	}
	if db.Spans[1].Ready <= db.Spans[1].From {
		t.Errorf("db's second start has no ready mark: %+v", db.Spans[1])
	}
	api := lanes["api"]
	if len(api.Marks) != 1 || api.Marks[0].Kind != "crash" || len(api.Spans) != 1 || api.Spans[0].Open {
		t.Errorf("api = %+v", api)
	}
	if len(lanes["cache"].Spans) != 0 {
		t.Errorf("cache was quiet: %+v", lanes["cache"])
	}
	if tl.Events[0].Text[:3] != "api" || len(tl.Events) != 5 {
		t.Errorf("events newest first, in the span only: %+v", tl.Events)
	}
	if tl.Ticks[6].Label != "now" {
		t.Errorf("ticks = %+v", tl.Ticks)
	}
}
