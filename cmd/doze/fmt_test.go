package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fmt lays out doze.hcl and its *.doze.hcl siblings; --check only reports, and
// fails while anything would change; a file that does not parse is left alone.
func TestFmtFormatsAndChecks(t *testing.T) {
	stack, home := t.TempDir(), t.TempDir()
	messy := "app \"web\" {\nrun = \"serve\"\n    port=8080\n}\n"
	tidy := "app \"web\" {\n  run  = \"serve\"\n  port = 8080\n}\n"
	write := func(name, src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(stack, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(stack, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	write("doze.hcl", messy)
	write("db.doze.hcl", "module postgres \"db\" { version = 18 }\n")
	write("notes.hcl", messy) // not part of the stack: never touched

	code, out, _ := dozeAt(t, stack, home, "fmt", "--check")
	if code != 1 || strings.TrimSpace(out) != "doze.hcl" {
		t.Fatalf("--check: exit %d, out %q; want exit 1 naming doze.hcl", code, out)
	}
	if read("doze.hcl") != messy {
		t.Fatal("--check wrote the file")
	}

	if code, out, errOut := dozeAt(t, stack, home, "fmt"); code != 0 || strings.TrimSpace(out) != "doze.hcl" {
		t.Fatalf("fmt: exit %d, out %q, err %q", code, out, errOut)
	}
	if got := read("doze.hcl"); got != tidy {
		t.Errorf("formatted:\n%s\nwant:\n%s", got, tidy)
	}
	if read("notes.hcl") != messy {
		t.Error("fmt touched a file that is not part of the stack")
	}
	if code, out, _ := dozeAt(t, stack, home, "fmt", "--check"); code != 0 || out != "" {
		t.Errorf("--check after fmt: exit %d, out %q; want 0 and nothing", code, out)
	}

	write("broken.doze.hcl", "app \"x\" {\n")
	if code, _, errOut := dozeAt(t, stack, home, "fmt"); code == 0 || !strings.Contains(errOut, "broken.doze.hcl") {
		t.Errorf("a file that does not parse: exit %d, err %q", code, errOut)
	}
	if read("broken.doze.hcl") != "app \"x\" {\n" {
		t.Error("a file that does not parse was rewritten")
	}
}
