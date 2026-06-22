package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeAndProjectLayout(t *testing.T) {
	t.Setenv("DOZE_HOME", "/srv/doze")
	cfg, err := Parse([]byte(`fake "x" { version = 1 }`), "/work/myapp/doze.hcl")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Home != "/srv/doze" {
		t.Errorf("Home = %q, want /srv/doze (from DOZE_HOME)", cfg.Home)
	}
	// Cache is shared under the home; per-engine toolchains live at <home>/<engine>.
	if cfg.CacheDir() != "/srv/doze/cache" {
		t.Errorf("CacheDir = %q", cfg.CacheDir())
	}
	// Project state is namespaced under the home.
	if !strings.HasPrefix(cfg.ProjectDir(), "/srv/doze/projects/myapp-") {
		t.Errorf("ProjectDir = %q, want /srv/doze/projects/myapp-<hash>", cfg.ProjectDir())
	}
	if cfg.ClusterDir("app") != filepath.Join(cfg.ProjectDir(), "clusters", "app") {
		t.Errorf("ClusterDir = %q", cfg.ClusterDir("app"))
	}
	if cfg.SocketDir("app") != filepath.Join(cfg.ProjectDir(), "run", "app") {
		t.Errorf("SocketDir = %q", cfg.SocketDir("app"))
	}
}

func TestProjectsDoNotCollide(t *testing.T) {
	t.Setenv("DOZE_HOME", "/srv/doze")
	// Two different projects with the same directory base name must get
	// distinct project dirs (the hash disambiguates).
	a, _ := Parse([]byte(`fake "x" { version = 1 }`), "/work/a/api/doze.hcl")
	b, _ := Parse([]byte(`fake "x" { version = 1 }`), "/work/b/api/doze.hcl")
	if a.ProjectDir() == b.ProjectDir() {
		t.Fatalf("distinct projects collided on %q", a.ProjectDir())
	}
	if !strings.Contains(a.ProjectDir(), "/api-") || !strings.Contains(b.ProjectDir(), "/api-") {
		t.Errorf("slugs should keep the readable base name: %q %q", a.ProjectDir(), b.ProjectDir())
	}
}

func TestExplicitDataDirOverridesNamespacing(t *testing.T) {
	t.Setenv("DOZE_HOME", "/srv/doze")
	cfg, err := Parse([]byte(`
data_dir = "/tmp/ephemeral"
fake "x" { version = 1 }
`), "/work/myapp/doze.hcl")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectDir() != "/tmp/ephemeral" {
		t.Errorf("explicit data_dir should win: %q", cfg.ProjectDir())
	}
	// The shared home is unaffected by an explicit project data_dir.
	if cfg.Home != "/srv/doze" {
		t.Errorf("Home should stay /srv/doze: %q", cfg.Home)
	}
}

func TestSanitizeSlug(t *testing.T) {
	cases := map[string]string{
		"MyApp":     "myapp",
		"my_app.v2": "my-app-v2",
		"--weird--": "weird",
		"":          "project",
		"a..b__c":   "a-b-c",
	}
	for in, want := range cases {
		if got := sanitizeSlug(in); got != want {
			t.Errorf("sanitizeSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
