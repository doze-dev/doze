package dash

import (
	"strings"
	"testing"
	"time"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/control/fakedaemon"
)

// connecting is a dash just started on a daemon that has not answered, with
// its clock at the given time after it opened.
func connecting(after time.Duration, o Options) *Model {
	o.Now, o.Loc = func() time.Time { return epoch }, time.UTC
	m := New(&fakedaemon.Fake{}, o)
	m.conn, m.waiting = connConnecting, true
	m.nowf = func() time.Time { return epoch.Add(after) }
	return m
}

// A daemon that answers at once never shows the splash: until splashAfter the
// screen is empty, so nothing flashes on the way to the page.
func TestTheSplashWaitsBeforeItShows(t *testing.T) {
	m := connecting(100*time.Millisecond, Options{Plain: true})
	if out := strings.TrimSpace(frame(m, 100, 24)); out != "" {
		t.Errorf("the screen before splashAfter:\n%s", out)
	}
}

// After that, the wordmark and that the dash is connecting, centred.
func TestTheSplashShowsTheWordmark(t *testing.T) {
	m := connecting(time.Second, Options{Plain: true})
	out := frame(m, 100, 24)
	for _, l := range wordmark {
		if !strings.Contains(out, strings.TrimRight(l, " ")) {
			t.Fatalf("the wordmark line %q is missing:\n%s", l, out)
		}
	}
	if !strings.Contains(out, "connecting to the daemon") {
		t.Errorf("the splash does not say it is connecting:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	first := -1
	for i, l := range lines {
		if strings.Contains(l, "██╗") {
			first = i
			break
		}
	}
	if first < 5 || first > 9 {
		t.Errorf("the wordmark starts on row %d of 24, not near the middle:\n%s", first, out)
	}
	if lead := len(lines[first]) - len(strings.TrimLeft(lines[first], " ")); lead < 25 {
		t.Errorf("the wordmark is not centred: %d columns in", lead)
	}
}

// The z's under the wordmark come and go: one, two, three, and again.
func TestTheSplashBreathes(t *testing.T) {
	var got []string
	for i := 0; i < 4; i++ {
		m := connecting(splashAfter+time.Duration(i)*breathEvery, Options{Plain: true})
		out := frame(m, 100, 24)
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, "connecting to the daemon") {
				got = append(got, strings.TrimSpace(strings.SplitN(l, "daemon", 2)[1]))
			}
		}
	}
	want := []string{"z", "z z", "z z z", "z"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the z's: %q, want %q", got, want)
	}
}

// The first answer ends it, a snapshot or an instances frame alike.
func TestTheFirstAnswerEndsTheSplash(t *testing.T) {
	m := connecting(time.Second, Options{Plain: true})
	m.Update(connMsg{up: true})
	m.Update(snapshotMsg{snap: control.Snapshot{Stack: "shop"}})
	if out := frame(m, 100, 24); strings.Contains(out, "██╗") || !strings.Contains(out, "Nothing is declared") {
		t.Errorf("after the snapshot:\n%s", out)
	}
	m = connecting(time.Second, Options{Plain: true})
	m.Update(frameMsg(control.Frame{Topic: control.TopicInstances}))
	if out := frame(m, 100, 24); strings.Contains(out, "██╗") {
		t.Errorf("after an instances frame:\n%s", out)
	}
}

// A daemon that can't be reached is not hidden behind the splash: the page says
// it is not responding, as it does once the stack is known.
func TestADownDaemonIsNotASplash(t *testing.T) {
	m := connecting(time.Second, Options{Plain: true})
	m.Update(connMsg{up: false, err: fakedaemon.ErrGone, retry: 2 * time.Second})
	if out := frame(m, 100, 24); strings.Contains(out, "██╗") || !strings.Contains(out, "not responding") {
		t.Errorf("a daemon that is down:\n%s", out)
	}
}

// Where the terminal draws only ASCII, the wordmark's blocks would come out as
// substitutes, so the name is one line instead.
func TestTheSplashInASCII(t *testing.T) {
	m := connecting(time.Second, Options{Plain: true, Glyphs: GlyphsASCII})
	out := frame(m, 100, 24)
	if strings.Contains(out, "|_") || strings.Count(out, "doze") != 1 || !strings.Contains(out, "connecting to the daemon") {
		t.Errorf("the ascii splash:\n%s", out)
	}
}

// The help screen opens with the wordmark and the version, where the window has
// room for them as well as every key.
func TestTheHelpScreenHasTheWordmark(t *testing.T) {
	m := board()
	m.version = "v0.2.0"
	m.modal = &modal{kind: modalHelp}
	out := frame(m, 140, 56)
	if !strings.Contains(out, strings.TrimSpace(wordmark[3])) || !strings.Contains(out, "doze v0.2.0") {
		t.Errorf("the help screen at 140x56:\n%s", out)
	}
	if !strings.Contains(out, "What the meters are") {
		t.Errorf("the keys are pushed out of sight:\n%s", out)
	}
	// a window the keys fill keeps them all in view, with no wordmark
	if out := frame(m, 140, 40); strings.Contains(out, "██╗") || !strings.Contains(out, "What the meters are") {
		t.Errorf("the help screen at 140x40:\n%s", out)
	}
	// and a terminal of ASCII gets none
	m.glyphs = glyphsASCII
	if out := frame(m, 140, 56); strings.Contains(out, "|_") || strings.Contains(out, "doze v0.2.0") {
		t.Errorf("the help screen in ascii:\n%s", out)
	}
}
