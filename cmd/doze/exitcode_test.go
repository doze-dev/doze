package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The test binary doubles as doze: run with DOZE_EXITCODE_CHILD=1, it is the
// CLI, so the table below checks real exit codes end to end.
func TestMain(m *testing.M) {
	if os.Getenv("DOZE_EXITCODE_CHILD") == "1" {
		os.Exit(realMain())
	}
	os.Exit(m.Run())
}

// doze runs the CLI in dir and returns its exit code and stderr.
func doze(t *testing.T, dir string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOZE_EXITCODE_CHILD=1", "DOZE_HOME="+t.TempDir(), "NO_COLOR=1")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), stderr.String()
	}
	if err != nil {
		t.Fatalf("doze %v: %v", args, err)
	}
	return 0, stderr.String()
}

func TestExitCodes(t *testing.T) {
	stack := t.TempDir()
	if err := os.WriteFile(filepath.Join(stack, "doze.hcl"), []byte("app \"api\" {\n  run = \"sleep 60\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, "doze.hcl"), []byte("app \"api\" {\n  rnu = \"x\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()

	for _, tc := range []struct {
		name string
		dir  string
		args []string
		code int
	}{
		{"a valid config lints", stack, []string{"lint"}, 0},
		{"an unknown command", stack, []string{"nosuch"}, 2},
		{"an unknown flag", stack, []string{"status", "--nosuch"}, 2},
		{"a malformed --var", stack, []string{"lint", "--var", "novalue"}, 2},
		{"an undeclared instance", stack, []string{"logs", "nosuch"}, 4},
		{"a config with an error", broken, []string{"lint"}, 5},
		{"no config anywhere", empty, []string{"lint"}, 6},
		{"following a stack that isn't running", stack, []string{"logs", "-f"}, 7},
		{"reset with no terminal to ask", stack, []string{"reset", "api"}, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stderr := doze(t, tc.dir, tc.args...)
			if code != tc.code {
				t.Errorf("doze %s: exit %d, want %d\n%s", strings.Join(tc.args, " "), code, tc.code, stderr)
			}
		})
	}
}

// Every code in the table is distinct, so a script can tell them apart.
func TestExitCodesAreDistinct(t *testing.T) {
	seen := map[int]bool{}
	kinds := map[string]bool{}
	for _, c := range exitCodes {
		if seen[c.code] || kinds[string(c.kind)] {
			t.Errorf("code %d (%s) repeats", c.code, c.kind)
		}
		seen[c.code], kinds[string(c.kind)] = true, true
	}
}

// The CLI reference lists every exit code with its kind, so it can't drift.
func TestExitCodesAreDocumented(t *testing.T) {
	doc, err := os.ReadFile("../../website/src/content/docs/reference/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range exitCodes {
		row := fmt.Sprintf("| %d | ", c.code)
		if c.kind != "" {
			row += "`" + string(c.kind) + "` |"
		}
		if !strings.Contains(string(doc), row) {
			t.Errorf("cli.md has no row %q", row)
		}
	}
}
