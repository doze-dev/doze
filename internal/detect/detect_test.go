package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appNamed(r Result, name string) *App {
	for i := range r.Apps {
		if r.Apps[i].Name == name {
			return &r.Apps[i]
		}
	}
	return nil
}

func moduleTypes(r Result) []string {
	var out []string
	for _, m := range r.Modules {
		out = append(out, m.Type)
	}
	return out
}

// The fixture stack, found again from nothing but its code.
func TestTheShopFixtureIsRecognised(t *testing.T) {
	r, err := Scan("../../examples/shop")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(r.Apps); got != 3 {
		t.Fatalf("apps = %+v", r.Apps)
	}
	api := appNamed(r, "api")
	if api == nil || api.Run != "go run ./cmd/api" {
		t.Fatalf("api = %+v", api)
	}
	want := map[string]string{"DATABASE_URL": "postgres", "CACHE_ADDR": "valkey", "KAFKA_BROKERS": "kafka"}
	for _, u := range api.Uses {
		if want[u.Env] != u.Kind {
			t.Errorf("api uses %+v", u)
		}
		delete(want, u.Env)
	}
	if len(want) != 0 {
		t.Errorf("api does not use %v: %+v", want, api.Uses)
	}
	got := map[string]bool{}
	for _, m := range moduleTypes(r) {
		got[m] = true
	}
	for _, w := range []string{"postgres", "valkey", "kafka", "aws"} {
		if !got[w] {
			t.Errorf("module %s not found: %v", w, moduleTypes(r))
		}
	}
}

func TestComposeAndMakefileAndEnv(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "docker-compose.yml", `
services:
  db:
    image: postgres:16
    ports: ["5432:5432"]
  cache:
    image: redis:7
  search:
    image: getmeili/meilisearch:v1.7
    ports: ["7700:7700"]
  mongo:
    image: mongo:7
  app:
    build: .
`)
	write(t, dir, "Makefile", "build:\n\tgo build\n\nmigrate:\n\tgoose up\n\nseed:\n\t./seed.sh\n\nclean:\n\trm -rf out\n")
	write(t, dir, ".env", "A=1\n")
	r, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := moduleTypes(r); len(got) != 2 || got[0] != "postgres" || got[1] != "valkey" {
		t.Errorf("modules = %v", got)
	}
	var svc []string
	for _, s := range r.Services {
		svc = append(svc, s.Name)
	}
	if len(svc) != 2 || svc[0] != "mongo" || svc[1] != "search" {
		t.Errorf("services = %v (an app that builds is not one)", svc)
	}
	if len(r.Tasks) != 2 || r.Tasks[0].Name != "migrate" || r.Tasks[0].Manual || r.Tasks[1].Name != "seed" || !r.Tasks[1].Manual {
		t.Errorf("tasks = %+v (build and clean are not preparation)", r.Tasks)
	}
	if r.EnvFile != ".env" {
		t.Errorf("env file = %q", r.EnvFile)
	}
}

func TestNodeProject(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"name":"storefront","scripts":{"dev":"vite","build":"vite build"},"dependencies":{"pg":"8","ioredis":"5"}}`)
	write(t, dir, "pnpm-lock.yaml", "")
	write(t, dir, "src/db.ts", `const url = process.env.X; getenv("DATABASE_URL")`)
	r, _ := Scan(dir)
	if len(r.Apps) != 1 || r.Apps[0].Name != "storefront" || r.Apps[0].Run != "pnpm run dev" {
		t.Fatalf("apps = %+v", r.Apps)
	}
	if got := moduleTypes(r); len(got) != 2 {
		t.Errorf("modules = %v", got)
	}
}

func TestNothingToFindIsNothing(t *testing.T) {
	r, err := Scan(t.TempDir())
	if err != nil || len(r.Apps)+len(r.Modules)+len(r.Services)+len(r.Tasks) != 0 {
		t.Fatalf("an empty directory suggested %+v, %v", r, err)
	}
}

func TestScaffoldOfTheShopFixtureParsesAndIsWired(t *testing.T) {
	r, _ := Scan("../../examples/shop")
	src := r.Scaffold(Versions{"postgres": "16", "valkey": "9", "kafka": "4"}, nil)
	for _, want := range []string{
		`stack = "shop"`,
		`module postgres "db" {`, `module valkey "cache" {`, `module kafka "events" {`, `module aws "cloud" {`,
		`app "api" {`, `run = "go run ./cmd/api"`,
		"DATABASE_URL = module.db.url", "CACHE_ADDR = module.cache.address", "KAFKA_BROKERS = module.events.address",
	} {
		if !contains2(src, want) {
			t.Errorf("the scaffold lacks %q:\n%s", want, src)
		}
	}
}

func contains2(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
