package config_test

import (
	"strings"
	"testing"

	"github.com/doze-dev/doze/internal/config"
)

const hand = `# my stack — hand written, and it should stay that way
stack = "shop"

# the database
module postgres "db" {
  version = 16   # pinned
  port    = 5432
}

app "api" {
  run = "go run ./cmd/api"
}
`

func TestAddingABlockKeepsWhatIsThere(t *testing.T) {
	e, err := config.OpenEdit([]byte(hand))
	if err != nil {
		t.Fatal(err)
	}
	if !e.Declared("db") || !e.Declared("api") || e.Declared("cache") {
		t.Fatalf("declared: db %v api %v cache %v", e.Declared("db"), e.Declared("api"), e.Declared("cache"))
	}
	if err := e.AppendBlock("module valkey \"cache\" {\n  version=9\n  port=6379\n}"); err != nil {
		t.Fatal(err)
	}
	out := string(e.Bytes())
	for _, want := range []string{
		"# my stack — hand written, and it should stay that way",
		"# the database", "version = 16 # pinned",
		`module valkey "cache" {`, "  version = 9", "  port    = 6379",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the edited file lacks %q:\n%s", want, out)
		}
	}
	if !strings.HasPrefix(out, strings.Replace(hand[:len(hand)-1], "16   # pinned", "16 # pinned", 1)) {
		t.Errorf("what was there is not left alone:\n%s", out)
	}
	if _, err := config.ParseShape([]byte(out), "/work/doze.hcl"); err != nil {
		t.Fatalf("the result should parse: %v", err)
	}
}

func TestAnEmptyFileTakesAStackNameFirst(t *testing.T) {
	e, _ := config.OpenEdit(nil)
	e.SetStack("demo")
	if err := e.AppendBlock(`app "api" { run = "x" }`); err == nil {
		// single-line blocks hold one argument; run alone is fine
	}
	out := string(e.Bytes())
	if !strings.HasPrefix(out, "stack = \"demo\"\n") {
		t.Fatalf("out:\n%s", out)
	}

	// With blocks already there, the stack line still goes on top.
	e2, _ := config.OpenEdit([]byte("module postgres \"db\" {\n  version = 16\n  port = 5432\n}\n"))
	e2.SetStack("demo")
	if out := string(e2.Bytes()); !strings.HasPrefix(out, "stack = \"demo\"\n") || !strings.Contains(out, `module postgres "db"`) {
		t.Fatalf("out:\n%s", out)
	}
	// And it is only set once.
	e2.SetStack("other")
	if out := string(e2.Bytes()); strings.Contains(out, "other") {
		t.Fatalf("a second SetStack changed it:\n%s", out)
	}
}

func TestABlockThatDoesNotParseIsRefused(t *testing.T) {
	e, _ := config.OpenEdit([]byte(hand))
	before := string(e.Bytes())
	if err := e.AppendBlock(`module postgres "x" {`); err == nil {
		t.Fatal("an unterminated block must not reach the file")
	}
	if err := e.AppendBlock("a = 1\nb = 2"); err == nil {
		t.Fatal("attributes are not a block")
	}
	if string(e.Bytes()) != before {
		t.Fatalf("a refused edit changed the file:\n%s", e.Bytes())
	}
}

func TestFingerprintsFollowTheConfigAndNothingElse(t *testing.T) {
	load := func(src string) map[string]config.BlockPrint {
		cfg, err := config.ParseShape([]byte(src), "/work/doze.hcl")
		if err != nil {
			t.Fatal(err)
		}
		return cfg.BlockFingerprints()
	}
	base := load(`
stack = "s"
app "api" {
  run = "go run ./cmd/api"
  env = { A = "1", B = "2" }
}
app "web" {
  run = "go run ./cmd/web"
}
`)
	same := load(`
# a comment, and the env keys in another order
stack = "s"

app "web" {
  run = "go run ./cmd/web"
}
app "api" {
  env = { B = "2", A = "1" }
  run = "go run ./cmd/api"
}
`)
	for _, name := range []string{"api", "web"} {
		if base[name].Fingerprint != same[name].Fingerprint {
			t.Errorf("%s: reordering and a comment changed its fingerprint", name)
		}
	}
	changed := load(`
stack = "s"
app "api" {
  run = "go run ./cmd/api -v"
  env = { A = "1", B = "2" }
}
app "web" {
  run = "go run ./cmd/web"
}
`)
	if base["api"].Fingerprint == changed["api"].Fingerprint {
		t.Error("changing run did not change api's fingerprint")
	}
	if base["web"].Fingerprint != changed["web"].Fingerprint {
		t.Error("changing api changed web's fingerprint")
	}
}
