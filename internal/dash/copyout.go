package dash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Getting text out of the dash. The clipboard goes by OSC 52, a request to the
// terminal that it may ignore, and nothing tells the program whether it did:
// Terminal.app, tmux without its clipboard setting, and some ssh paths drop it.
// So the dash says what it did (sent it), not what happened (copied it), says so
// when the terminal is one that is likely to ignore it, and has a second way
// that cannot be ignored: write a file and say where.

// clipboardLikely says whether the terminal is one known to honour OSC 52 and
// the session is not one that sits between the dash and it (tmux, screen).
func clipboardLikely() bool {
	if os.Getenv("TMUX") != "" || os.Getenv("STY") != "" {
		return false
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "ghostty", "WezTerm", "iTerm.app", "vscode", "Hyper":
		return true
	}
	term := os.Getenv("TERM")
	for _, p := range []string{"xterm-kitty", "xterm-ghostty", "alacritty", "foot", "wezterm"} {
		if strings.HasPrefix(term, p) {
			return true
		}
	}
	return os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("ALACRITTY_LOG") != ""
}

// out sends text to the clipboard, or, when file is set, to a file, and says
// which. what names it for the message ("12 lines", "api's address"), and name
// is what the file is called.
func (m *Model) out(text, what, name string, file bool) tea.Cmd {
	if file {
		path := filepath.Join(os.TempDir(), fmt.Sprintf("doze-%s-%s.txt", safeName(name), m.now.Format("150405")))
		if err := os.WriteFile(path, []byte(text+"\n"), 0o600); err != nil {
			m.notice = "could not write " + path + ": " + err.Error()
			return nil
		}
		m.notice = "wrote " + what + " to " + path
		return nil
	}
	m.notice = "sent " + what + " to the clipboard"
	if !clipboardLikely() {
		m.notice += " · if nothing pastes, Y writes a file instead"
	}
	return tea.SetClipboard(text)
}

func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "copy"
	}
	return b.String()
}

func lineWord(n int) string {
	if n == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", n)
}
