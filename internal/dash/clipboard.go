package dash

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// How text reaches the clipboard. A request to the terminal (OSC 52) may be
// ignored without anyone knowing, so the first choice, on a machine you are
// sitting at, is the system's own clipboard tool, which either works or says it
// did not. OSC 52 is for the rest: over ssh, where the clipboard that matters is
// on the other side, and where no tool is installed. Inside tmux the text also
// goes to tmux's paste buffer. This is how Claude Code does it too
// (code.claude.com/docs/en/fullscreen, "Keep native text selection").

var errNoClipboardTool = errors.New("no clipboard tool found")

// copyNative copies text with the system's clipboard tool and says which it
// used. It is a variable so a test never touches the real clipboard.
var copyNative = nativeCopy

// copyTmux puts text in tmux's paste buffer (and, where tmux is set to, the
// terminal's clipboard).
var copyTmux = tmuxBuffer

// overSSH says whether this session is on the far end of an ssh connection, so
// the system clipboard here is not the one the person is at.
func overSSH() bool {
	return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CLIENT") != ""
}

type clipTool struct {
	cmd  string
	args []string
}

// clipTools are the tools to try on this system, in order.
func clipTools() []clipTool {
	switch runtime.GOOS {
	case "darwin":
		return []clipTool{{"pbcopy", nil}}
	case "windows":
		return []clipTool{{"clip.exe", nil}}
	}
	var ts []clipTool
	if isWSL() {
		ts = append(ts, clipTool{"clip.exe", nil})
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		ts = append(ts, clipTool{"wl-copy", nil})
	}
	return append(ts, clipTool{"xclip", []string{"-selection", "clipboard", "-in"}}, clipTool{"xsel", []string{"--clipboard", "--input"}})
}

func isWSL() bool {
	b, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}

func nativeCopy(text string) (string, error) {
	for _, t := range clipTools() {
		path, err := exec.LookPath(t.cmd)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := exec.CommandContext(ctx, path, t.args...)
		cmd.Stdin = strings.NewReader(text)
		err = cmd.Run()
		cancel()
		if err == nil {
			return strings.TrimSuffix(t.cmd, ".exe"), nil
		}
	}
	return "", errNoClipboardTool
}

func tmuxBuffer(text string) error {
	path, err := exec.LookPath("tmux")
	if err != nil || os.Getenv("TMUX") == "" {
		return errNoClipboardTool
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "load-buffer", "-w", "-")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// nativeSelectKey is the key that hands the mouse back to the terminal for one
// drag, which differs by terminal.
func nativeSelectKey() string {
	switch os.Getenv("TERM_PROGRAM") {
	case "Apple_Terminal":
		return "Fn"
	case "iTerm.app":
		return "Option"
	}
	return "Shift"
}
