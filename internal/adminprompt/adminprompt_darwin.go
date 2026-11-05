package adminprompt

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

const supported = true

// command is osascript asking for the password and running the script as
// root. The script and the sentence go in as arguments to the run
// handler, never spliced into AppleScript source, so nothing in them
// needs AppleScript quoting.
func command(what, script string) *exec.Cmd {
	return exec.Command("/usr/bin/osascript",
		"-e", "on run argv",
		"-e", "do shell script (item 1 of argv) with prompt (item 2 of argv) with administrator privileges",
		"-e", "end run",
		"/bin/sh -c "+shellQuote(script), Prompt(what))
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

// classify turns osascript's failure into ErrCancelled, or into what the
// script said. osascript reports "<pos>: execution error: <message> (<code>)",
// and -128 is "User canceled."
func classify(err error, stderr string) error {
	msg := strings.TrimSpace(stderr)
	if strings.Contains(msg, "(-128)") {
		return ErrCancelled
	}
	if _, after, ok := strings.Cut(msg, "execution error: "); ok {
		msg = after
	}
	if i := strings.LastIndex(msg, " ("); i > 0 && strings.HasSuffix(msg, ")") {
		msg = msg[:i]
	}
	if msg == "" {
		return err
	}
	return errors.New(msg)
}
