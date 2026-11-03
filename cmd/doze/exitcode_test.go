package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doze-dev/doze/internal/fault"
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

// dozeRun runs the CLI in dir with extra environment and returns its exit
// code, stdout and stderr.
func dozeRun(t *testing.T, dir string, env []string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "DOZE_EXITCODE_CHILD=1", "DOZE_HOME="+t.TempDir(), "NO_COLOR=1"), env...)
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

// --json gives a program one envelope on stdout, success or failure, and
// nothing else there.
func TestJSONEnvelope(t *testing.T) {
	stack := t.TempDir()
	if err := os.WriteFile(filepath.Join(stack, "doze.hcl"), []byte("app \"api\" {\n  run = \"sleep 60\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, "doze.hcl"), []byte("app \"api\" {\n  rnu = \"x\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	type env struct {
		Schema  string          `json:"schema"`
		Command string          `json:"command"`
		OK      bool            `json:"ok"`
		Data    json.RawMessage `json:"data"`
		Error   *errorBody      `json:"error"`
	}
	for _, tc := range []struct {
		name    string
		dir     string
		env     []string
		args    []string
		code    int
		command string
		errKind fault.Kind
		data    string // a substring the data must hold
	}{
		{"a result", stack, nil, []string{"--json", "lint"}, 0, "doze lint", "", `"app": 1`},
		{"DOZE_JSON=1", stack, []string{"DOZE_JSON=1"}, []string{"version"}, 0, "doze version", "", `"version"`},
		{"a failure", broken, nil, []string{"--json", "lint"}, 5, "doze lint", fault.Invalid, ""},
		{"a usage mistake", stack, nil, []string{"--json", "nosuch"}, 2, "doze", fault.Usage, ""},
		{"a command with no JSON result", stack, nil, []string{"--json", "init"}, 2, "doze init", fault.Usage, ""},
		{"logs", stack, nil, []string{"--json", "logs", "api"}, 0, "doze logs", "", `"lines": []`},
		{"an undeclared name", stack, nil, []string{"--json", "url", "nosuch"}, 4, "doze url", fault.NotFound, ""},
		{"env", stack, nil, []string{"--json", "env"}, 0, "doze env", "", `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, stderr := dozeRun(t, tc.dir, tc.env, tc.args...)
			if code != tc.code {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", code, tc.code, out, stderr)
			}
			var e env
			dec := json.NewDecoder(strings.NewReader(out))
			if err := dec.Decode(&e); err != nil {
				t.Fatalf("stdout is not one envelope: %v\n%s", err, out)
			}
			if dec.More() {
				t.Fatalf("stdout holds more than the envelope:\n%s", out)
			}
			if e.Schema != "doze/v1" || e.Command != tc.command || e.OK != (tc.code == 0) {
				t.Errorf("envelope = %+v", e)
			}
			if tc.errKind != "" && (e.Error == nil || e.Error.Code != tc.errKind || e.Error.Exit != tc.code) {
				t.Errorf("error = %+v, want %s exit %d", e.Error, tc.errKind, tc.code)
			}
			if !strings.Contains(string(e.Data), tc.data) {
				t.Errorf("data %s lacks %s", e.Data, tc.data)
			}
		})
	}
}

// DOZE_JSON=1 never breaks a command with no JSON result; -q prints nothing;
// flags that contradict each other are a usage mistake.
func TestOutputFlags(t *testing.T) {
	stack := t.TempDir()
	if err := os.WriteFile(filepath.Join(stack, "doze.hcl"), []byte("app \"api\" {\n  run = \"sleep 60\"\n  port = 4711\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := dozeRun(t, stack, []string{"DOZE_JSON=1"}, "url", "nosuch"); code != 4 || !strings.HasPrefix(out, "{") {
		t.Errorf("DOZE_JSON=1 url: exit %d, out %q", code, out)
	}
	if code, out, stderr := dozeRun(t, stack, []string{"DOZE_JSON=1"}, "init"); code == 2 || strings.HasPrefix(out, "{") {
		t.Errorf("DOZE_JSON=1 init: exit %d, out %q, stderr %q", code, out, stderr)
	}
	if code, out, _ := dozeRun(t, stack, nil, "-q", "url", "api"); code != 0 || out != "" {
		t.Errorf("-q url: exit %d, out %q", code, out)
	}
	for _, args := range [][]string{{"--json", "-q", "lint"}, {"--timeout", "0s", "lint"}} {
		if code, _, _ := dozeRun(t, stack, nil, args...); code != 2 {
			t.Errorf("doze %v: exit %d, want 2", args, code)
		}
	}
}
