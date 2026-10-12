package dash

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
)

// C8 of the audit: what a very large stack costs the dash. 500 instances each
// replaying a full backlog of 400 lines is 200,000 lines; the figures are
// logged, and the bounds are loose: they catch a regression, not a budget.
func TestFiveHundredInstancesOfBacklogCostsLittle(t *testing.T) {
	m := stressModel(500, func(i int) string { return fmt.Sprintf("svc%03d", i) }, nil)
	m.w, m.h = 150, 40
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	text := "INFO GET /orders/ord_1042 200 6ms request_id=7f3a9c2e-5d41-4c0b-9a77-0e2b6d1f8a35 user=u_12345"
	for i := 0; i < 500; i++ {
		name := fmt.Sprintf("svc%03d", i)
		for j := 0; j < logsKept; j++ {
			m.Update(logMsg{name, control.LogFrame{Instance: name, Line: fmt.Sprintf("%s #%d-%d", text, i, j), Time: epoch.Add(time.Duration(j) * time.Second), Run: 1}})
		}
	}
	fill := time.Since(start)
	if n := len(m.cache["svc007"]); n != logsKept {
		t.Fatalf("kept %d lines of svc007", n)
	}
	start = time.Now()
	m.Render(150, 40)
	first := time.Since(start)
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(m)
	held := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("200,000 lines: %d MB held, %v to take them in, %v to draw a frame", held>>20, fill.Round(time.Millisecond), first.Round(time.Millisecond))
	if held > 150<<20 {
		t.Errorf("held %d MB", held>>20)
	}
}

// A burst: tens of thousands of lines a second into one instance, with the
// stack's logs page open and the screen drawn every hundred lines (a renderer
// that keeps up can be no faster than that). What is held stays at its caps,
// and the cost of a line stays small.
func TestALogBurstIsBoundedInMemoryAndCost(t *testing.T) {
	m := stressModel(12, func(i int) string { return fmt.Sprintf("svc%02d", i) }, nil)
	m.w, m.h = 150, 40
	m.page = pageLogs
	m.openLogs()
	const n = 50000
	start := time.Now()
	var worst time.Duration
	for i := 0; i < n; i++ {
		m.Update(logMsg{"svc03", control.LogFrame{Instance: "svc03", Line: fmt.Sprintf("INFO burst line %d with some payload to be a realistic width of text", i), Time: epoch.Add(time.Duration(i) * time.Millisecond), Run: 1}})
		if i%100 == 0 {
			t0 := time.Now()
			m.Render(150, 40)
			if d := time.Since(t0); d > worst {
				worst = d
			}
		}
	}
	total := time.Since(start)
	t.Logf("%d lines: %v in all, worst frame %v, kept %d and %d on the page", n, total.Round(time.Millisecond), worst.Round(time.Millisecond), len(m.cache["svc03"]), len(m.all))
	if len(m.cache["svc03"]) > logsKept || len(m.all) > allKept {
		t.Errorf("not bounded: %d, %d", len(m.cache["svc03"]), len(m.all))
	}
	if worst > 250*time.Millisecond {
		t.Errorf("a frame took %v", worst)
	}
}
