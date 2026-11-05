package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dozeAt runs the CLI in dir with a fixed home, so several commands can
// see the same authority and record, and returns the exit code, stdout
// and stderr.
func dozeAt(t *testing.T, dir, home string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Dir = dir
	cmd.Env = append(cleanEnv(), "DOZE_EXITCODE_CHILD=1", "DOZE_HOME="+home, "NO_COLOR=1")
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), stdout.String(), stderr.String()
	}
	if err != nil {
		t.Fatalf("doze %v: %v", args, err)
	}
	return 0, stdout.String(), stderr.String()
}

// The surfaces that only report on the authority never make one; the one
// that sets the machine up does; and what points a shell at it never
// points the store-replacing variables at the root alone.
func TestTrustFlagsOnlyLookUnlessTheyAreAskedToSetUp(t *testing.T) {
	stack, home := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(stack, "doze.hcl"), []byte("stack = \"shop\"\ndefaults { domains = true }\napp \"web\" {\n  run     = \"sleep 60\"\n  port    = 28082\n  domains = [\"dev.acme.example\", \"*.dev.acme.example\"]\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, "ca", "root.crt")
	exists := func() bool { _, err := os.Stat(root); return err == nil }

	if code, _, _ := dozeAt(t, stack, home, "trust", "--rotate", "--remove"); code == 0 {
		t.Error("--rotate with --remove is refused")
	}
	code, out, _ := dozeAt(t, stack, home, "trust", "--check")
	if code == 0 {
		t.Errorf("nothing is set up yet, so --check fails:\n%s", out)
	}
	if exists() {
		t.Fatal("--check made an authority")
	}
	if code, out, _ := dozeAt(t, stack, home, "trust", "--remove", "--print"); code != 0 || exists() {
		t.Errorf("--remove --print changes nothing (exit %d, authority made %v):\n%s", code, exists(), out)
	}
	if code, out, _ := dozeAt(t, stack, home, "env"); code != 0 || strings.Contains(out, "CA_FILE") {
		t.Errorf("with no authority, env has nothing to point at (exit %d):\n%s", code, out)
	}

	code, out, _ = dozeAt(t, stack, home, "trust", "--print")
	if code != 0 || !exists() {
		t.Fatalf("--print makes the authority the script trusts (exit %d):\n%s", code, out)
	}
	if !strings.Contains(out, "dev.acme.example") || !strings.Contains(out, "root.crt") {
		t.Errorf("the script routes the domain and trusts the root:\n%s", out)
	}
	if strings.Contains(out, "*.dev.acme.example") && !strings.Contains(out, "# route dev.acme.example") {
		t.Errorf("a wildcard is routed as the name under it:\n%s", out)
	}

	code, out, _ = dozeAt(t, stack, home, "env")
	if code != 0 {
		t.Fatalf("env: exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "DOZE_CA_FILE='"+root+"'") {
		t.Errorf("env points DOZE_CA_FILE at the root:\n%s", out)
	}
	if !strings.Contains(out, "SSL_CERT_FILE='"+filepath.Join(home, "ca", "bundle.pem")+"'") || strings.Contains(out, "SSL_CERT_FILE='"+root+"'") {
		t.Errorf("env points SSL_CERT_FILE at the bundle, never the root alone:\n%s", out)
	}

	code, out, _ = dozeAt(t, stack, home, "trust", "--check")
	if code == 0 || !strings.Contains(out, "certificate authority") {
		t.Errorf("--check reports the authority is not trusted yet (exit %d):\n%s", code, out)
	}

	// A broken authority is reported, and --check points at --rotate rather
	// than at a setup that would fail on it.
	if err := os.WriteFile(filepath.Join(home, "ca", "root.key"), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ = dozeAt(t, stack, home, "trust", "--check")
	if code == 0 || !strings.Contains(out, "--rotate") {
		t.Errorf("a broken authority sends the person to --rotate (exit %d):\n%s", code, out)
	}
	if code, _, errOut := dozeAt(t, stack, home, "trust", "--print"); code == 0 || !strings.Contains(errOut, "rotate") {
		t.Errorf("setting up over a broken authority is refused with the remedy (exit %d): %s", code, errOut)
	}
}
