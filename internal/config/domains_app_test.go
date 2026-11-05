package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppDomainsAreDeclaredCheckedAndListed(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) (*Config, error) {
		path := filepath.Join(dir, "doze.hcl")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return Load(path, nil)
	}
	cfg, err := write(`stack = "shop"
defaults { domains = true }
app "web" {
  run     = "pnpm dev"
  port    = 3000
  domains = ["dev.acme.com", "*.dev.acme.com"]
}
`)
	if err != nil {
		t.Fatal(err)
	}
	web := cfg.Lookup("web")
	if web == nil || len(web.Domains) != 2 || web.Domains[1] != "*.dev.acme.com" || web.Cert != "" {
		t.Fatalf("web = %+v", web)
	}
	if got := cfg.AppDomains(); len(got) != 2 {
		t.Errorf("AppDomains = %v", got)
	}
	for _, bad := range []struct{ body, want string }{
		{"stack = \"shop\"\ndefaults { domains = true }\napp \"web\" {\n  run = \"x\"\n  port = 3000\n  domains = [\"web.shop.doze\"]\n}\n", "doze name already"},
		{"stack = \"shop\"\ndefaults { domains = true }\napp \"web\" {\n  run = \"x\"\n  port = 3000\n  domains = [\"app.localhost\"]\n}\n", "localhost"},
		{"stack = \"shop\"\napp \"web\" {\n  run = \"x\"\n  port = 3000\n  domains = [\"dev.acme.com\"]\n}\n", "domains = true"},
		{"stack = \"shop\"\ndefaults { domains = true }\napp \"web\" {\n  run = \"x\"\n  port = 3000\n  domains = [\"dev.acme.com\"]\n}\napp \"api\" {\n  run = \"y\"\n  port = 3001\n  domains = [\"dev.acme.com\"]\n}\n", "domain conflict"},
		{"stack = \"shop\"\ndefaults { domains = true }\napp \"web\" {\n  run = \"x\"\n  port = 3000\n  domains = [\"dev.acme.com\"]\n  cert = \"missing.crt\"\n  key = \"missing.key\"\n}\n", "not a file"},
		{"stack = \"shop\"\ndefaults { domains = true }\napp \"web\" {\n  run = \"x\"\n  port = 3000\n  domains = [\"dev.acme.com\"]\n  cert = \"a.crt\"\n}\n", "both"},
		{"stack = \"shop\"\ndefaults { domains = true }\nservice \"db\" {\n  run = \"x\"\n  port = 5432\n  domains = [\"db.acme.com\"]\n}\n", "not expected"},
	} {
		if _, err := write(bad.body); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("want an error with %q, got %v", bad.want, err)
		}
	}
}

func TestABarePublicSuffixIsNotADomainOfYours(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"*.co.uk", "com", "*.github.io"} {
		body := "stack = \"shop\"\ndefaults { domains = true }\napp \"web\" {\n  run = \"x\"\n  port = 3000\n  domains = [\"" + bad + "\"]\n}\n"
		path := filepath.Join(dir, "doze.hcl")
		_ = os.WriteFile(path, []byte(body), 0o644)
		if _, err := Load(path, nil); err == nil || (!strings.Contains(err.Error(), "public suffix") && !strings.Contains(err.Error(), "no top-level domain")) {
			t.Errorf("%q: want a public-suffix error, got %v", bad, err)
		}
	}
	body := "stack = \"shop\"\ndefaults { domains = true }\napp \"web\" {\n  run = \"x\"\n  port = 3000\n  domains = [\"app.github.io\", \"*.dev.acme.co.uk\"]\n}\n"
	path := filepath.Join(dir, "doze.hcl")
	_ = os.WriteFile(path, []byte(body), 0o644)
	if _, err := Load(path, nil); err != nil {
		t.Errorf("a name under a suffix is fine: %v", err)
	}
}
