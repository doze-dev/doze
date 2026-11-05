package adminprompt

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
)

// supported is a desktop session with pkexec: polkit asks for the password
// in a window of its own. Over SSH, or on a server, there is no agent to
// ask, and the terminal's sudo is the way.
var supported = func() bool {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	_, err := exec.LookPath("pkexec")
	return err == nil
}()

// command is pkexec running the script as root. polkit's own dialog says
// which program asks; the sentence goes to the log, since pkexec has no
// place for it without a policy file.
func command(_, script string) *exec.Cmd {
	return exec.Command("pkexec", "/bin/sh", "-c", script)
}

func run(what, script string) error {
	c := command(what, script)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return classify(err, stderr.String())
	}
	return nil
}

// classify turns pkexec's failure into ErrCancelled, or into what the
// script said. pkexec exits 126 when the dialog was dismissed and 127 when
// authorisation failed.
func classify(err error, stderr string) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) && (exit.ExitCode() == 126 || exit.ExitCode() == 127) && !strings.Contains(stderr, "not found") {
		return ErrCancelled
	}
	if msg := strings.TrimSpace(stderr); msg != "" {
		return errors.New(msg)
	}
	return err
}
