package main

import (
	"testing"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/modules"
)

// Whatever `doze init` writes has to be a config doze reads: every starter
// block, from the offline seed and from the catalog, with and without an app.
func TestInitWritesAConfigDozeReads(t *testing.T) {
	offers := append([]initEngine{}, initSeed...)
	offers = append(offers,
		initEngine{key: "mariadb", block: blockFromCatalog(modules.CatalogEntry{Name: "mariadb", CatalogModule: modules.CatalogModule{EngineVersions: []string{"10", "11"}, Port: 3306}})},
		initEngine{key: "temporal", block: blockFromCatalog(modules.CatalogEntry{Name: "temporal", CatalogModule: modules.CatalogModule{Label: "dev"}})},
	)
	var all []string
	for _, e := range offers {
		all = append(all, e.key)
	}
	for _, app := range []string{"", "go run ./cmd/api"} {
		src := scaffoldFor(all, app, offers)
		cfg, err := config.ParseShape([]byte(src), "doze.hcl")
		if err != nil {
			t.Fatalf("init wrote a config that does not lint (app %q): %v\n%s", app, err, src)
		}
		if len(cfg.Instances) != len(offers)+len(appLabel(app)) {
			t.Fatalf("declared %d blocks, want %d\n%s", len(cfg.Instances), len(offers)+len(appLabel(app)), src)
		}
	}
}
