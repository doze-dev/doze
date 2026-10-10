package dash

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/dash/core"
	"github.com/doze-dev/doze/internal/journal"
)

// session drives a dash the way the terminal would: keys in, commands
// settled, a frame out. A stream is not played: a test feeds the lines
// itself, which is what makes its output the same every run.
type session struct {
	t    *testing.T
	m    *Model
	w, h int
}

func newSession(t *testing.T, d control.Daemon, w, h int) *session {
	t.Helper()
	m := New(d, Options{Dark: true, Plain: true, Now: func() time.Time { return epoch }, Loc: time.UTC})
	s := &session{t: t, m: m, w: w, h: h}
	s.send(tea.WindowSizeMsg{Width: w, Height: h})
	snap, _ := d.Snapshot(nil) //nolint:staticcheck
	s.send(snapshotMsg{snap: snap})
	return s
}

func (s *session) send(msg tea.Msg) {
	next, cmd := s.m.Update(msg)
	s.m = next.(*Model)
	s.settle(cmd)
}

// settle runs a command and feeds its message back, recursively, except for
// the ones that wait on a stream.
func (s *session) settle(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch out := cmd().(type) {
	case nil, frameMsg, tickMsg:
	case tea.BatchMsg:
		for _, c := range out {
			s.settle(c)
		}
	default:
		s.send(out)
	}
}

func (s *session) key(keys ...string) {
	for _, k := range keys {
		var key tea.Key
		switch k {
		case "enter":
			key = tea.Key{Code: tea.KeyEnter}
		case "esc":
			key = tea.Key{Code: tea.KeyEscape}
		case "up":
			key = tea.Key{Code: tea.KeyUp}
		case "down":
			key = tea.Key{Code: tea.KeyDown}
		case "tab":
			key = tea.Key{Code: tea.KeyTab}
		default:
			key = tea.Key{Text: k, Code: []rune(k)[0]}
		}
		s.send(tea.KeyPressMsg(key))
	}
}

func (s *session) logs(id string, lines ...control.LogFrame) {
	for _, l := range lines {
		s.send(logMsg{ID: id, Frame: l})
	}
}

func (s *session) screen() string {
	var out []string
	for _, l := range strings.Split(ansi.Strip(s.m.Render(s.w, s.h)), "\n") {
		out = append(out, strings.TrimRight(l, " "))
	}
	return strings.Join(out, "\n") + "\n"
}

var apiLines = []control.LogFrame{
	{Instance: "api", Run: 3, Line: "starting api 1.4.2", Stream: "o", Time: epoch.Add(-9 * time.Minute)},
	{Instance: "api", Run: 3, Line: "INFO listening on :8080", Stream: "o", Time: epoch.Add(-9*time.Minute + time.Second)},
	{Instance: "api", Run: 3, Line: "DEBUG cache warmed in 4ms", Stream: "o", Time: epoch.Add(-8 * time.Minute)},
	{Instance: "api", Run: 3, Line: "level=warn msg=\"slow query\" took=412ms", Stream: "o", Time: epoch.Add(-5 * time.Minute)},
	{Instance: "api", Run: 3, Line: "GET /orders 200 14ms", Stream: "o", Time: epoch.Add(-2 * time.Minute)},
	{Instance: "api", Run: 3, Line: "ERROR orders: connection reset by peer", Stream: "e", Time: epoch.Add(-70 * time.Second)},
	{Instance: "api", Run: 3, Line: "retrying in 1s", Stream: "e", Time: epoch.Add(-69 * time.Second)},
	{Instance: "api", Run: 3, Line: "GET /orders 200 11ms", Stream: "o", Time: epoch.Add(-30 * time.Second)},
}

func TestFrameShowsTheLogsOfAnApp(t *testing.T) {
	s := newSession(t, shop(), 120, 36)
	s.key("enter") // the api row is first
	s.logs("logs:api", apiLines...)
	golden(t, "frame_logs_120x36", s.screen())
}

func TestScrollingPausesAndFollowResumes(t *testing.T) {
	s := newSession(t, shop(), 120, 12)
	s.key("enter")
	s.logs("logs:api", apiLines...)
	s.key("up", "up")
	if out := s.screen(); !strings.Contains(out, "paused, 2 lines below") {
		t.Fatalf("scrolling up should pause:\n%s", out)
	}
	// A new line arrives while paused: what is on screen stays where it is.
	s.logs("logs:api", control.LogFrame{Instance: "api", Run: 3, Line: "GET /health 200 1ms", Time: epoch})
	if out := s.screen(); !strings.Contains(out, "paused, 3 lines below") {
		t.Fatalf("a line while paused should add to what is below:\n%s", out)
	}
	s.key("f")
	if out := s.screen(); !strings.Contains(out, "live") || !strings.Contains(out, "GET /health 200 1ms") {
		t.Fatalf("f should follow again:\n%s", out)
	}
}

func TestEscGoesBackToHome(t *testing.T) {
	s := newSession(t, shop(), 120, 36)
	s.key("enter")
	if out := s.screen(); !strings.Contains(out, "‹ api") {
		t.Fatalf("enter should open the frame:\n%s", out)
	}
	s.key("esc")
	if out := s.screen(); !strings.Contains(out, "Apps  3") {
		t.Fatalf("esc should go back to home:\n%s", out)
	}
}

// Levels are read from what the program wrote, and counted.
func TestLogLevelsAreCounted(t *testing.T) {
	s := newSession(t, shop(), 120, 36)
	s.key("enter")
	s.logs("logs:api", apiLines...)
	out := s.screen()
	if !strings.Contains(out, "1 error") || !strings.Contains(out, "1 warning") {
		t.Fatalf("the status line should count levels:\n%s", out)
	}
}

var _ = core.Resized{}

func (s *session) events(id string, es ...journal.Event) {
	for _, e := range es {
		s.send(eventMsg{ID: id, Event: e})
	}
}

func TestOverviewOfAModule(t *testing.T) {
	d := shop()
	s := newSession(t, d, 120, 36)
	// Home orders apps, then modules: cache, cloud, db, events.
	s.key("j", "j", "j", "j", "j", "enter")
	s.events("overview:db",
		journal.Event{Kind: journal.Wake, Instance: "db", Time: epoch.Add(-20 * time.Minute), Cause: journal.Cause{Kind: journal.ByDependency, By: "api"}},
		journal.Event{Kind: journal.Ready, Instance: "db", Time: epoch.Add(-20*time.Minute + 66*time.Millisecond), Took: 66 * time.Millisecond},
		journal.Event{Kind: journal.Sleep, Instance: "db", Time: epoch.Add(-9 * time.Minute), Cause: journal.Cause{Kind: journal.ByIdle}},
		journal.Event{Kind: journal.Wake, Instance: "db", Time: epoch.Add(-3 * time.Minute), Cause: journal.Cause{Kind: journal.ByConnection}},
		journal.Event{Kind: journal.Ready, Instance: "db", Time: epoch.Add(-3*time.Minute + 71*time.Millisecond), Took: 71 * time.Millisecond},
	)
	golden(t, "frame_overview_120x36", s.screen())
}

func TestLastRunOfATask(t *testing.T) {
	d := shop()
	d.Lines["migrate"] = []control.LogFrame{
		{Instance: "migrate", Run: 4, Line: "applying 001_init.sql", Stream: "o", Time: epoch.Add(-12 * time.Minute)},
		{Instance: "migrate", Run: 4, Line: "applied 1 migration", Stream: "o", Time: epoch.Add(-12 * time.Minute)},
	}
	s := newSession(t, d, 120, 36)
	// …events, then services (mailpit), then tasks: migrate is the last but one.
	for i := 0; i < 11; i++ {
		s.key("j")
	}
	// The tasks group: migrate then seed.
	s.key("k")
	s.key("enter")
	s.events("lastrun:migrate",
		journal.Event{Kind: journal.Wake, Instance: "migrate", Time: epoch.Add(-3 * time.Hour), Detail: "first run"},
		journal.Event{Kind: journal.Ready, Instance: "migrate", Time: epoch.Add(-3 * time.Hour), Took: 380 * time.Millisecond},
		journal.Event{Kind: journal.Wake, Instance: "migrate", Time: epoch.Add(-12 * time.Minute), Detail: "inputs changed"},
		journal.Event{Kind: journal.Ready, Instance: "migrate", Time: epoch.Add(-12 * time.Minute), Took: 412 * time.Millisecond},
	)
	golden(t, "frame_lastrun_120x36", s.screen())
}

func TestTabKeySwitchesTabs(t *testing.T) {
	s := newSession(t, shop(), 120, 36)
	s.key("enter") // an app opens on its logs
	s.key("tab")
	if out := s.screen(); !strings.Contains(out, "awake because") {
		t.Fatalf("tab should show the overview:\n%s", out)
	}
}

func stackLines() []control.LogFrame {
	return []control.LogFrame{
		{Instance: "api", Line: "INFO listening on :8080", Time: epoch.Add(-9 * time.Minute)},
		{Instance: "db", Line: "database system is ready to accept connections", Time: epoch.Add(-9*time.Minute + time.Second)},
		{Instance: "worker", Line: "INFO consuming orders", Time: epoch.Add(-8 * time.Minute)},
		{Instance: "api", Line: "GET /orders 200 14ms", Time: epoch.Add(-5 * time.Minute)},
		{Instance: "worker", Line: "WARN order 7713 retried", Time: epoch.Add(-4 * time.Minute)},
		{Instance: "db", Line: "ERROR out of shared memory", Time: epoch.Add(-3 * time.Minute)},
		{Instance: "api", Line: "GET /orders 500 31ms", Time: epoch.Add(-3*time.Minute + time.Second)},
	}
}

func TestStackLogsMergeInTimeOrder(t *testing.T) {
	s := newSession(t, shop(), 120, 36)
	s.key("L")
	// Lines arrive out of order across sources, as a merged stream does.
	ls := stackLines()
	s.logs("stacklogs", ls[3], ls[0], ls[5], ls[1], ls[6], ls[2], ls[4])
	golden(t, "stacklogs_120x36", s.screen())
}

func TestStackLogsSearchTogglesAndLevel(t *testing.T) {
	s := newSession(t, shop(), 120, 36)
	s.key("L")
	for _, l := range stackLines() {
		s.logs("stacklogs", l)
	}

	// Search: the matches are counted and the query is shown.
	s.key("/", "o", "r", "d", "e", "r", "enter")
	out := s.screen()
	if !strings.Contains(out, "4 matches for order") {
		t.Fatalf("search should count matches:\n%s", out)
	}

	// Toggle the first source off (api): its lines go.
	s.key("1")
	if out := s.screen(); strings.Contains(out, "GET /orders") {
		t.Fatalf("toggling api off should hide its lines:\n%s", out)
	}
	s.key("a")

	// Level: warn shows warnings and errors only.
	s.key("e")
	out = s.screen()
	if strings.Contains(out, "listening on") || !strings.Contains(out, "WARN order 7713") || !strings.Contains(out, "ERROR out of shared") {
		t.Fatalf("level warn should keep only warn and error:\n%s", out)
	}
}
