package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/portholder"
)

// inProject points the CLI at a fresh directory and offline starters.
func inProject(t *testing.T, doze string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DOZE_MODULES", "off")
	old := configPath
	configPath = filepath.Join(dir, "doze.hcl")
	t.Cleanup(func() { configPath = old })
	if doze != "" {
		if err := os.WriteFile(configPath, []byte(doze), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func read(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// phases.md exit measure: `doze add postgres` and `doze add mongo` each
// produce a file that passes lint.
func TestAddPostgresAndMongoProduceFilesThatLint(t *testing.T) {
	inProject(t, "")
	if err := runAdd("postgres", "", "", "", 0, false); err != nil {
		t.Fatal(err)
	}
	out := read(t)
	for _, want := range []string{`module postgres "db" {`, "version = 18", "port    = 5432"} {
		if !strings.Contains(out, want) {
			t.Errorf("the file lacks %q:\n%s", want, out)
		}
	}
	if _, err := config.ParseShape([]byte(out), configPath); err != nil {
		t.Fatalf("doze add postgres: the file does not lint: %v\n%s", err, out)
	}

	// mongo is not a module: a service, as a container when mongod is not here.
	t.Setenv("PATH", t.TempDir())
	if err := runAdd("mongo", "", "", "", 0, false); err != nil {
		t.Fatal(err)
	}
	out = read(t)
	for _, want := range []string{`module postgres "db"`, `service "mongo" {`, "docker run --rm -p $HOST:$PORT:27017 mongo:7", "port = 27017"} {
		if !strings.Contains(out, want) {
			t.Errorf("the file lacks %q:\n%s", want, out)
		}
	}
	if _, err := config.ParseShape([]byte(out), configPath); err != nil {
		t.Fatalf("doze add mongo: the file does not lint: %v\n%s", err, out)
	}
}

func TestAddKeepsWhatIsThereAndTakesAFreeName(t *testing.T) {
	inProject(t, "# mine\nstack = \"shop\"\n\nmodule postgres \"db\" {\n  version = 16\n  port    = 5432\n}\n")
	if err := runAdd("postgres", "", "", "", 5433, false); err != nil {
		t.Fatal(err)
	}
	out := read(t)
	if !strings.HasPrefix(out, "# mine\nstack = \"shop\"") || !strings.Contains(out, `module postgres "db2" {`) || !strings.Contains(out, "port    = 5433") {
		t.Fatalf("out:\n%s", out)
	}
	// An explicit name that is taken is refused, and nothing changes.
	before := read(t)
	if err := runAdd("valkey", "db", "", "", 0, false); err == nil {
		t.Fatal("a name that is taken must be refused")
	}
	if read(t) != before {
		t.Fatal("a refused add changed the file")
	}
}

func TestAddDryRunWritesNothingAndAServiceCanBeInstalledOrDownloaded(t *testing.T) {
	inProject(t, "")
	if err := runAdd("valkey", "", "", "", 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configPath); err == nil {
		t.Fatal("--dry-run wrote a file")
	}

	t.Setenv("PATH", t.TempDir())
	if err := runAdd("mongo", "", "", "installed", 0, false); err == nil {
		t.Fatal("--as installed with no mongod on the PATH should say so")
	}
	if err := runAdd("mongo", "", "", "download", 0, false); err != nil {
		t.Fatal(err)
	}
	if out := read(t); !strings.Contains(out, "download = ") || !strings.Contains(out, "fill this in") {
		t.Fatalf("a download service says what is left to do:\n%s", out)
	}
}

func TestAddAServiceFromABinaryOnThePath(t *testing.T) {
	inProject(t, "")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "mongod"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if err := runAdd("mongo", "", "", "", 0, false); err != nil {
		t.Fatal(err)
	}
	if out := read(t); !strings.Contains(out, `"mongod --dbpath $DATA --bind_ip $HOST --port $PORT"`) {
		t.Fatalf("a mongod on the PATH is run directly:\n%s", out)
	}
}

// copyTree copies a directory, leaving out the names given.
func copyTree(t *testing.T, from, to string, skip ...string) {
	t.Helper()
	err := filepath.Walk(from, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		for _, s := range skip {
			if rel == s {
				return nil
			}
		}
		dst := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// phases.md exit measure: `doze init` in a copy of examples/shop with its
// doze.hcl removed recreates a file that runs the stack.
func TestInitRecreatesTheShopStack(t *testing.T) {
	dir := inProject(t, "")
	copyTree(t, "../../examples/shop", dir, "doze.hcl", "doze.lock")
	if _, err := os.Stat(filepath.Join(dir, "doze.hcl")); err == nil {
		t.Fatal("the copy still has its doze.hcl")
	}
	t.Setenv("PATH", t.TempDir())

	done, err := initFromDetection(true)
	if err != nil || !done {
		t.Fatalf("init = %v, %v", done, err)
	}
	cfg, err := config.ParseShape([]byte(read(t)), configPath)
	if err != nil {
		t.Fatalf("the recreated file does not parse: %v\n%s", err, read(t))
	}
	want := map[string]string{
		"api": "app", "worker": "app", "web": "app",
		"db": "module", "cache": "module", "events": "module", "cloud": "module",
	}
	got := map[string]string{}
	for _, d := range cfg.Instances {
		got[d.Name] = d.Kind
	}
	for n, k := range want {
		if got[n] != k {
			t.Errorf("%s: want a %s, got %q\n%s", n, k, got[n], read(t))
		}
	}
	// And the apps are wired to what they use: the same references the
	// hand-written file has.
	out := strings.Join(strings.Fields(read(t)), " ") // the file is aligned; compare without the padding
	for _, line := range []string{"DATABASE_URL = module.db.url", "CACHE_ADDR = module.cache.address", "KAFKA_BROKERS = module.events.address"} {
		if !strings.Contains(out, line) {
			t.Errorf("the file lacks %q:\n%s", line, out)
		}
	}
	// An app depends on what it references: api must boot db, cache and events.
	api := cfg.Lookup("api")
	if api == nil || len(api.Deps) < 3 {
		t.Errorf("api's dependencies = %+v", api)
	}
}

func TestInitFindsNothingAndSaysSo(t *testing.T) {
	inProject(t, "")
	done, err := initFromDetection(true)
	if err != nil || done {
		t.Fatalf("an empty project: done = %v, err = %v — it should leave the wizard to ask", done, err)
	}
}

func TestDoctorSaysWhoHoldsAPortAndWhatToDo(t *testing.T) {
	inProject(t, "stack = \"p\"\n\ndefaults {\n  domains = false\n}\n\nmodule postgres \"db\" {\n  version = 16\n  port    = 5432\n}\n\napp \"web\" {\n  run  = \"x\"\n  port = 8080\n}\n")
	cfg, err := config.ParseShape([]byte(read(t)), configPath)
	if err != nil {
		t.Fatal(err)
	}
	who := func(port int) (portholder.Holder, bool) {
		if port == 5432 {
			return portholder.Holder{PID: 412, Command: "postgres", Addr: "127.0.0.1:5432"}, true
		}
		return portholder.Holder{}, false
	}
	got := takenPorts(cfg, false, 0, who)
	if len(got) != 1 || got[0].port != 5432 || got[0].name != "db" {
		t.Fatalf("taken = %+v", got)
	}
	advice := portAdvice(got[0], false)
	for _, want := range []string{"held by postgres (pid 412) on 127.0.0.1:5432", "kill 412", "doze doctor --fix", `"db" block`} {
		if !strings.Contains(advice, want) {
			t.Errorf("advice lacks %q:\n%s", want, advice)
		}
	}
	// What the daemon itself holds is not a conflict.
	if got := takenPorts(cfg, false, 412, who); len(got) != 0 {
		t.Errorf("the daemon's own port was reported: %+v", got)
	}
	// With an address each there is nothing to clash with — when names are on.
	cfg.Defaults.Domains = true
	if got := takenPorts(cfg, true, 0, who); len(got) != 0 {
		t.Errorf("ports do not clash when each service has an address: %+v", got)
	}
	if strings.Contains(portAdvice(got0(cfg, who), true), "doctor --fix") {
		t.Errorf("a machine that is set up is not told to set up")
	}
}

func got0(cfg *config.Config, who func(int) (portholder.Holder, bool)) portTaken {
	cfg.Defaults.Domains = false
	return takenPorts(cfg, false, 0, who)[0]
}
