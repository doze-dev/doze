package adminprompt

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// The script reaches the shell byte for byte, whatever quotes and
// heredocs it holds, and the sentence is its own argument.
func TestTheScriptIsPassedAsAnArgumentAndQuotedOnce(t *testing.T) {
	script := "set -e\nprintf 'a\\nb'\ncat <<'PLIST'\n<x a=\"1\">it's</x>\nPLIST\necho \"$HOME\" `x` $(y)"
	c := command("route .doze", script)
	n := len(c.Args)
	if c.Args[n-1] != Prompt("route .doze") {
		t.Fatalf("last argument = %q, want the prompt", c.Args[n-1])
	}
	for _, a := range c.Args[:n-2] {
		if strings.Contains(a, "PLIST") {
			t.Fatal("the script leaked into the AppleScript source")
		}
	}
	// What osascript hands to do shell script is a command line for sh:
	// what sh -c receives must be exactly the script, which printf shows.
	line := c.Args[n-2]
	quoted := strings.TrimPrefix(line, "/bin/sh -c ")
	out, err := exec.Command("/bin/sh", "-c", "printf %s "+quoted).Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != script {
		t.Fatalf("the shell saw\n%s\nwant\n%s", out, script)
	}
}

func TestCancelAndFailureAreTold(t *testing.T) {
	base := errors.New("exit status 1")
	if err := classify(base, "0:120: execution error: User canceled. (-128)\n"); !errors.Is(err, ErrCancelled) {
		t.Fatalf("cancel = %v", err)
	}
	if err := classify(base, "0:149: execution error: launchctl: no such file (5)\n"); err.Error() != "launchctl: no such file" {
		t.Fatalf("failure = %q", err)
	}
	if err := classify(base, ""); err != base {
		t.Fatalf("silent failure = %v", err)
	}
}

func TestPrompt(t *testing.T) {
	if got := Prompt("route .doze to its resolver."); got != "doze needs an administrator's password to route .doze to its resolver." {
		t.Fatalf("Prompt = %q", got)
	}
}
