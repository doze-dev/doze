// Package adminprompt runs a privileged script behind the system's own
// administrator prompt, for when doze has no terminal to ask sudo from: the
// app, and the doze commands the app runs.
package adminprompt

import (
	"errors"
	"os"
	"strings"
)

// Env is set by the app on the doze commands it runs, so that their
// privileged step asks with the system's prompt instead of sudo.
const Env = "DOZE_ADMIN_PROMPT"

// CancelledText is what a cancelled prompt says, also in the output of a
// doze command the app ran, which is how the app tells it from a failure.
const CancelledText = "the administrator prompt was cancelled"

// ErrCancelled is the person closing the prompt without a password.
var ErrCancelled = errors.New(CancelledText)

// Supported says this system has a prompt doze can use.
func Supported() bool { return supported }

// Requested says this process was asked, through Env, to use the prompt
// instead of sudo, and can.
func Requested() bool { return os.Getenv(Env) == "1" && supported }

// Run runs script as root, after the system's prompt for an
// administrator's password. what says what it is for, in a few words
// that finish "doze needs an administrator's password to …".
func Run(what, script string) error {
	if !supported {
		return errors.New("no administrator prompt on this system: run the command in a terminal")
	}
	return run(what, script)
}

// Prompt is the sentence the system's prompt shows.
func Prompt(what string) string {
	what = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(what), "."))
	if what == "" {
		return "doze needs an administrator's password to set this machine up."
	}
	return "doze needs an administrator's password to " + what + "."
}

// Quote renders s as one single-quoted shell word.
func Quote(s string) string { return shellQuote(s) }

// shellQuote renders s as one single-quoted shell word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
