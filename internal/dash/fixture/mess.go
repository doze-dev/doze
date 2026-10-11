package fixture

import (
	"fmt"
	"strings"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
)

// Mess is the stack the boards do not draw, and the one a real project is: a
// couple of dozen instances, names that run on, a crash with a stack trace, a
// task that failed, instances that have never run, logs in three formats and a
// line a thousand cells wide. A screen that only holds up for Shop is not done.
func Mess(now time.Time) *fakedaemon.Fake {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	f := &fakedaemon.Fake{Hold: true}
	var in []control.InstanceView
	add := func(v control.InstanceView) { in = append(in, v) }

	long := "customer-notifications-email-dispatcher-worker"
	add(control.InstanceView{Name: long, Kind: "app", Engine: "process", State: "active", Run: "node --enable-source-maps --max-old-space-size=2048 ./services/notifications/dist/dispatcher.js --concurrency 16 --queue email,sms,push", RAM: 612 << 20, CPU: 31.4, PID: 9001, StartedAt: ago(3 * time.Hour), Conns: 12})
	add(control.InstanceView{Name: "api-gateway", Kind: "app", Engine: "process", State: "crashing", Run: "go run ./cmd/gateway",
		LastError:    "panic: runtime error: invalid memory address or nil pointer dereference\n[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x4c2a1f]\n\ngoroutine 1 [running]:\nmain.(*Router).mount(0x0, {0xc0001a2000, 0x8})\n\t/Users/dev/shop/cmd/gateway/router.go:212 +0x1f\nmain.main()\n\t/Users/dev/shop/cmd/gateway/main.go:57 +0x2a5\nexit status 2",
		RestartCount: 4, RestartMax: 5, NextRestart: now.Add(8 * time.Second)})
	for i := 1; i <= 9; i++ {
		st := "active"
		if i%3 == 0 {
			st = "reaped"
		}
		add(control.InstanceView{Name: fmt.Sprintf("svc-%02d", i), Kind: "app", Engine: "process", State: st, Run: fmt.Sprintf("./bin/svc-%02d --port $PORT", i),
			RAM: int64(20+i*13) << 20, CPU: float64(i) / 3, PID: 9100 + i, StartedAt: ago(time.Duration(i) * 7 * time.Minute)})
	}
	add(control.InstanceView{Name: "orders-db", Kind: "module", Engine: "postgres", Version: "17", State: "active", RAM: 310 << 20, CPU: 4.2, PID: 700, Conns: 38,
		StartedAt: ago(5 * time.Hour), URL: "postgres://app@orders-db.mess.doze:5432/orders", Holders: []control.HolderView{{Kind: "dependant", Name: long}, {Kind: "connection", Count: 38}, {Kind: "pin"}}})
	add(control.InstanceView{Name: "search-index", Kind: "module", Engine: "opensearch", Version: "2.13", State: "booting", Needs: []string{"orders-db"}})
	add(control.InstanceView{Name: "events", Kind: "module", Engine: "kafka", State: "reaped"})
	add(control.InstanceView{Name: "sessions", Kind: "module", Engine: "valkey", Version: "9", State: "idle", RAM: 18 << 20, IdleSince: ago(40 * time.Second)})
	for _, n := range []string{"imgproxy", "mailpit", "minio", "keycloak"} {
		add(control.InstanceView{Name: n, Kind: "service", Engine: "process", State: "reaped", Run: "docker run --rm -p 8080:8080 --env-file .env.local registry.example.com/platform/" + n + ":2026.10.1"})
	}
	add(control.InstanceView{Name: "migrate", Kind: "task", Engine: "process", State: "reaped",
		Task: &control.TaskView{Result: "failed", ExitCode: 3, TookMS: 412, RanAt: ago(9 * time.Minute), Why: "migrations/0042_add_index_concurrently.sql changed"}})
	add(control.InstanceView{Name: "seed", Kind: "task", Engine: "process", State: "reaped"})
	add(control.InstanceView{Name: "codegen", Kind: "task", Engine: "process", State: "reaped", Task: &control.TaskView{Result: "done", TookMS: 8200, RanAt: ago(3 * time.Hour), Why: "doze up"}})
	f.Snap = control.Snapshot{Stack: "platform-monorepo", Dir: "~/code/work/platform-monorepo-with-a-long-name", IdleTimeout: 5 * time.Minute, Instances: in}

	for i := 0; i < 240; i++ {
		var st []control.StatView
		for _, v := range in {
			if v.State == "active" || v.State == "idle" {
				st = append(st, control.StatView{Name: v.Name, PID: v.PID, RAM: v.RAM + int64(i%17)<<18, CPU: v.CPU + float64(i%9)/4, Conns: v.Conns})
			}
		}
		f.Frames = append(f.Frames, control.Frame{Topic: control.TopicStats, Stats: st})
	}

	if f.Lines == nil {
		f.Lines = map[string][]control.LogFrame{}
	}
	line := func(inst string, d time.Duration, s string) {
		l := control.LogFrame{Instance: inst, Line: s, Time: ago(d), Run: 1}
		f.Frames = append(f.Frames, control.Frame{Topic: control.TopicLogs, Log: &l})
		f.Lines[inst] = append(f.Lines[inst], l)
	}
	for i := 0; i < 60; i++ {
		d := time.Duration(60-i) * 20 * time.Second
		switch i % 6 {
		case 0:
			line(long, d, fmt.Sprintf(`{"level":30,"time":%d,"msg":"dispatched","queue":"email","id":"msg_%05d","took_ms":%d}`, i, i, 20+i%40))
		case 1:
			line(long, d, fmt.Sprintf("level=info msg=\"batch sent\" size=%d queue=sms", 16+i))
		case 2:
			line(long, d, fmt.Sprintf("2026-10-12T01:%02d:00Z WARN provider rate limited, backing off %dms", i%60, 250*(i%4+1)))
		case 3:
			line(long, d, fmt.Sprintf("GET /healthz 200 %dms", i%7))
		case 4:
			line(long, d, `{"level":"error","msg":"push failed","err":"apns: BadDeviceToken","device":"`+strings.Repeat("a1b2c3d4", 6)+`"}`)
		default:
			line(long, d, fmt.Sprintf("handled %d jobs", i))
		}
	}
	line(long, 5*time.Second, "INFO "+strings.Repeat("a very long line that goes on and on ", 30))
	line("api-gateway", 30*time.Second, "panic: runtime error: invalid memory address or nil pointer dereference")
	line("migrate", 9*time.Minute, "ERROR: cannot run CREATE INDEX CONCURRENTLY inside a transaction block")
	return f
}
