package config

import (
	"strings"
	"testing"

	"github.com/doze-dev/doze-sdk/engine"
)

// netDriver stands in for a module that declares listeners: a tcp broker, a
// console on its name and a registry on a child, like the kafka module.
type netDriver struct{ fakeDriver }

func (netDriver) Type() string { return "netmod" }
func (netDriver) Network(engine.Instance) engine.Network {
	return engine.Network{Listeners: []engine.Listener{
		{Name: engine.MainListener, Protocol: engine.ProtocolTCP},
		{Name: "console", Port: 9093, Protocol: engine.ProtocolHTTP, Routes: []string{"@"}},
		{Name: "registry", Port: 8081, Protocol: engine.ProtocolHTTP, Routes: []string{"registry", "sync-@"}},
	}}
}

func init() { engine.Register(netDriver{}) }

func TestAnInstancesGrantWithNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "doze.hcl", `
stack = "shop"
defaults { names = true }
module netmod "events" {
  version = 1
  port    = 9092
}
`)
	cfg, err := Load(dir+"/doze.hcl", nil)
	if err != nil {
		t.Fatal(err)
	}
	decl := cfg.Lookup("events")
	g, err := cfg.Grant(decl, netDriver{}, "/s/main.sock")
	if err != nil {
		t.Fatal(err)
	}
	if g.Host != "events.shop.doze" {
		t.Errorf("host = %q", g.Host)
	}
	if l := g.Listeners["registry"]; l.Addr != "events.shop.doze:8081" || l.URL != "http://registry.events.shop.doze" || len(l.Hosts) != 2 || l.Hosts[1] != "sync-events.shop.doze" {
		t.Errorf("registry = %+v", l)
	}
	if l := g.Listeners[engine.MainListener]; l.Addr != "events.shop.doze:9092" || l.Backend != "/s/main.sock" {
		t.Errorf("main = %+v", l)
	}
}

func TestAnInstancesGrantWithoutNamesOrRoutes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "doze.hcl", `
module netmod "events" {
  version = 1
  port    = 9092
}
`)
	cfg, err := Load(dir+"/doze.hcl", nil)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := cfg.Grant(cfg.Lookup("events"), netDriver{}, "/s/main.sock")
	if g.Named() || g.Listeners["console"].Addr != "127.0.0.1:9093" || len(g.Listeners["console"].Hosts) != 0 {
		t.Errorf("without names = %+v", g)
	}

	writeFile(t, dir, "doze.hcl", `
defaults { names = true }
module netmod "events" {
  version = 1
  port    = 9092
  routes  = false
}
`)
	cfg, err = Load(dir+"/doze.hcl", nil)
	if err != nil {
		t.Fatal(err)
	}
	g, _ = cfg.Grant(cfg.Lookup("events"), netDriver{}, "/s/main.sock")
	if l := g.Listeners["console"]; !g.Named() || len(l.Hosts) != 0 || l.URL != "http://events.doze-test.doze:9093" && !strings.HasSuffix(l.URL, ".doze:9093") {
		t.Errorf("routes = false = %+v", g.Listeners["console"])
	}
}

// Without names every listener of every instance is on 127.0.0.1, so a second
// port of one instance collides with another instance's port like any two.
func TestListenersCollideWithoutNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "doze.hcl", `
module netmod "events" {
  version = 1
  port    = 9092
}
module fake "web" {
  version = 1
  port    = 9093
}
`)
	_, err := Load(dir+"/doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), `"events"`) || !strings.Contains(err.Error(), `"web"`) || !strings.Contains(err.Error(), "9093") {
		t.Fatalf("err = %v, want the console of events and web named", err)
	}
}

// A route's name may not be another instance's name: a sibling route
// sync-<name> against an instance called sync-<name>.
func TestARouteMayNotBeAnotherInstancesName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "doze.hcl", `
stack = "shop"
defaults { names = true }
module netmod "events" {
  version = 1
  port    = 9092
}
module fake "sync-events" {
  version = 1
  port    = 7000
}
`)
	_, err := Load(dir+"/doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "sync-events.shop.doze") || !strings.Contains(err.Error(), `"events"`) {
		t.Fatalf("err = %v, want the clash named", err)
	}
}

// A listener on the instance's own port: the person's port wins, and the error
// says which listener.
func TestAListenerOnTheDeclaredPort(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "doze.hcl", `
module netmod "events" {
  version = 1
  port    = 8081
}
`)
	_, err := Load(dir+"/doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), `"registry"`) || !strings.Contains(err.Error(), "8081") {
		t.Fatalf("err = %v", err)
	}
}
