package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveLoadAndList(t *testing.T) {
	home := t.TempDir()
	shop := filepath.Join(home, "projects", "shop-1")
	blog := filepath.Join(home, "projects", "blog-2")
	if _, ok, err := Load(shop); ok || err != nil {
		t.Fatalf("an unrecorded project: ok %v, err %v", ok, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := Update(shop, func(p *Project) {
		p.Config, p.Name, p.LastUp, p.LastSet = "/code/shop/doze.hcl", "shop", now, []string{"api"}
	}); err != nil {
		t.Fatal(err)
	}
	if err := Update(blog, func(p *Project) { p.Config, p.LastUp = "/code/blog/doze.hcl", now.Add(-time.Hour) }); err != nil {
		t.Fatal(err)
	}
	p, ok, err := Load(shop)
	if err != nil || !ok || p.Config != "/code/shop/doze.hcl" || !p.LastUp.Equal(now) || len(p.LastSet) != 1 || p.Dir() != shop {
		t.Fatalf("load = %+v, %v, %v", p, ok, err)
	}
	// A broken record is skipped, not fatal.
	bad := filepath.Join(home, "projects", "bad-3")
	_ = os.MkdirAll(bad, 0o755)
	_ = os.WriteFile(filepath.Join(bad, FileName), []byte("{"), 0o644)
	list, err := List(home)
	if err != nil || len(list) != 2 || list[0].Name != "shop" {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

// A field a newer doze wrote survives a save by this one.
func TestUnknownFieldsAreKept(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, FileName), []byte(`{"schema":1,"config":"/x/doze.hcl","tabs":["home"]}`), 0o644)
	if err := Update(dir, func(p *Project) { p.LastSet = []string{"db"} }); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, FileName))
	if !strings.Contains(string(b), `"tabs":["home"]`) || !strings.Contains(string(b), `"last_set":["db"]`) {
		t.Fatalf("saved %s", b)
	}
}

// A record that can't be read is set aside and replaced on the next update.
func TestABrokenRecordIsReplaced(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, FileName), []byte(""), 0o644)
	if _, _, err := Load(dir); err == nil {
		t.Fatal("an empty record must not load")
	}
	if err := Update(dir, func(p *Project) { p.LastSet = []string{"db"} }); err != nil {
		t.Fatal(err)
	}
	if p, ok, err := Load(dir); err != nil || !ok || len(p.LastSet) != 1 {
		t.Fatalf("after update: %+v, %v, %v", p, ok, err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName+".bad")); err != nil {
		t.Fatalf("the broken record should be kept aside: %v", err)
	}
}
