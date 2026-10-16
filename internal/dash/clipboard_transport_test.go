package dash

import (
	"errors"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func withClipboard(t *testing.T, native func(string) (string, error), tmux func(string) error) {
	t.Helper()
	oldN, oldT := copyNative, copyTmux
	copyNative, copyTmux = native, tmux
	t.Cleanup(func() { copyNative, copyTmux = oldN, oldT })
	for _, k := range []string{"SSH_CONNECTION", "SSH_TTY", "SSH_CLIENT", "TMUX"} {
		t.Setenv(k, "")
	}
}

// On the machine you are at, the system's clipboard tool is used, and what is
// said is true: it was copied, and by what.
func TestCopyUsesTheSystemClipboardFirst(t *testing.T) {
	var got string
	withClipboard(t, func(s string) (string, error) { got = s; return "pbcopy", nil }, func(string) error { return errors.New("no") })
	m := board()
	cmd := m.out("line one\nline two", "2 lines", "x", false)
	if cmd != nil {
		t.Error("nothing should be sent to the terminal when the tool worked")
	}
	if got != "line one\nline two" || m.notice != "copied 2 lines to the clipboard · pbcopy" {
		t.Errorf("got %q, notice %q", got, m.notice)
	}
}

// If the tool fails, the terminal is asked instead, and it is said to be a
// request.
func TestCopyFallsBackToTheTerminal(t *testing.T) {
	withClipboard(t, func(string) (string, error) { return "", errNoClipboardTool }, func(string) error { return errors.New("no") })
	m := board()
	if cmd := m.out("x", "1 line", "x", false); cmd == nil {
		t.Error("the terminal should have been asked")
	}
	if !strings.HasPrefix(m.notice, "sent 1 line to the clipboard") {
		t.Errorf("notice %q", m.notice)
	}
}

// Over ssh the machine's own clipboard is the wrong one: the tool is not used.
func TestCopyOverSSHGoesToTheTerminal(t *testing.T) {
	called := false
	withClipboard(t, func(string) (string, error) { called = true; return "pbcopy", nil }, func(string) error { return errors.New("no") })
	t.Setenv("SSH_CONNECTION", "10.0.0.1 5000 10.0.0.2 22")
	m := board()
	if cmd := m.out("x", "1 line", "x", false); cmd == nil || called {
		t.Errorf("over ssh: cmd=%v, native called=%v", cmd != nil, called)
	}
}

// Inside tmux the text also goes to tmux's buffer, and that is said.
func TestCopyInsideTmuxAlsoFillsTheBuffer(t *testing.T) {
	var buf string
	withClipboard(t, func(string) (string, error) { return "pbcopy", nil }, func(s string) error { buf = s; return nil })
	t.Setenv("TMUX", "/tmp/tmux-501/default,1,0")
	m := board()
	m.out("abc", "1 line", "x", false)
	if buf != "abc" || !strings.Contains(m.notice, "and tmux's buffer") {
		t.Errorf("buffer %q, notice %q", buf, m.notice)
	}
}

// ctrl+c with a selection open copies it, as in an editor; it does not quit.
func TestCtrlCCopiesAnOpenSelection(t *testing.T) {
	var got string
	withClipboard(t, func(s string) (string, error) { got = s; return "pbcopy", nil }, func(string) error { return errors.New("no") })
	m := board()
	frame(m, 150, 44)
	m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if !m.selecting {
		t.Fatal("v should start a selection")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if got == "" || m.selecting {
		t.Errorf("ctrl+c did not copy and leave: got %q selecting=%v", got, m.selecting)
	}
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Error("ctrl+c quit the dash while a selection was open")
		}
	}
}

func TestTheNativeSelectKeyFollowsTheTerminal(t *testing.T) {
	for term, want := range map[string]string{"Apple_Terminal": "Fn", "iTerm.app": "Option", "ghostty": "Shift", "": "Shift"} {
		t.Setenv("TERM_PROGRAM", term)
		if got := nativeSelectKey(); got != want {
			t.Errorf("%q: %s, want %s", term, got, want)
		}
	}
}

func TestNoMouseGivesTheMouseBack(t *testing.T) {
	t.Setenv("DOZE_NO_MOUSE", "")
	if v := board().View(); v.MouseMode != tea.MouseModeCellMotion {
		t.Error("the mouse is captured by default")
	}
	t.Setenv("DOZE_NO_MOUSE", "1")
	if v := board().View(); v.MouseMode != tea.MouseModeNone {
		t.Error("DOZE_NO_MOUSE should leave the mouse to the terminal")
	}
}

// The real path: a clipboard tool that is run, fed the text on stdin, and named.
// A script on PATH stands in for it, so the test never touches the clipboard.
func TestNativeCopyRunsTheToolWithTheTextOnStdin(t *testing.T) {
	dir := t.TempDir()
	sink := dir + "/sink.txt"
	for _, tool := range clipTools() {
		script := "#!/bin/sh\n/bin/cat > '" + sink + "'\n"
		if err := os.WriteFile(dir+"/"+tool.cmd, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	via, err := nativeCopy("alpha\nbeta ✓")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(sink)
	if string(got) != "alpha\nbeta ✓" || via == "" {
		t.Errorf("via %q, tool got %q", via, got)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := nativeCopy("x"); !errors.Is(err, errNoClipboardTool) {
		t.Errorf("with no tool: %v", err)
	}
}
