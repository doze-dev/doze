package daemon

import (
	"net"
	"testing"

	names "github.com/doze-dev/doze-names"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/endpoints"
)

// Routes go into the shared registry, on the entry of the name they belong to,
// and nowhere else: one table, which the front door of whichever doze program
// holds :80 reads. An app's route is its own name; a module's are the names its
// listeners declare (listeners.go).
func TestRoutesArePublishedInTheSharedRegistry(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{StackName: "demo", Home: home, Defaults: config.Defaults{Domains: true}}
	cfg.Add(&config.InstanceDecl{Type: "process", Name: "api", Port: 28080, Enabled: true, Spec: appSpec(t, 28080)})
	zone := names.Open(home, "doze")
	d := &Daemon{cfg: cfg, logf: t.Logf, zone: zone, resources: map[string]string{},
		instRoutes: map[string]map[string]string{"aws": {"aws.demo.doze": "127.0.0.22:24566", "sync-aws.demo.doze": "127.0.0.22:24566"}}}

	plan := &bindPlan{
		bind:    map[string]string{"api": "127.0.0.21:28080", "aws": "127.0.0.22:24566"},
		resolve: map[string]net.IP{},
	}
	eps := []endpoints.Endpoint{{Name: "api", Engine: "process", Address: "127.0.0.1:28080", Domain: "api.demo.doze"}}

	// The names are claimed first, as the daemon does (a route name as a twin of
	// its instance's, at the same address), then routed.
	if _, err := zone.ClaimAt(names.Name{Host: "api.demo.doze", Tier: names.TierQualified}, net.ParseIP("127.0.0.21")); err != nil {
		t.Fatal(err)
	}
	l, err := zone.ClaimAt(names.Name{Host: "aws.demo.doze", Tier: names.TierQualified}, net.ParseIP("127.0.0.22"))
	if err != nil {
		t.Fatal(err)
	}
	if twins, _ := d.claimRouteNames(l, "aws.demo.doze"); len(twins) != 1 {
		t.Fatalf("twins = %d, want sync-aws.demo.doze", len(twins))
	}
	d.publishRoutes(eps, plan)

	got := zone.Snapshot()
	// Every route is a public address — doze's own listener — so a request
	// through the front door wakes the instance like any other connection.
	for host, want := range map[string]string{"api.demo.doze": "127.0.0.21:28080", "aws.demo.doze": "127.0.0.22:24566", "sync-aws.demo.doze": "127.0.0.22:24566"} {
		if e := got[host]; e.Target != want {
			t.Fatalf("%s route = %+v, want %s", host, e, want)
		}
	}
	if u := zone.URLFor("api.demo.doze"); u == "" {
		t.Fatal("a routed name must have a URL")
	}
}

// A name another program holds cannot be routed by this stack, and trying must
// not disturb the holder's route.
func TestANameHeldElsewhereIsNotRouted(t *testing.T) {
	home := t.TempDir()
	other := names.Open(home, "doze-aws")
	lease, err := other.Claim(names.Qualified("aws", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Route("127.0.0.2:4566"); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{StackName: "demo", Home: home, Defaults: config.Defaults{Domains: true}}
	d := &Daemon{cfg: cfg, logf: t.Logf, zone: names.Open(home, "doze")}
	// Same process in a test, so make the stack's handle a different owner by
	// routing a name it never claimed.
	d.instRoutes = map[string]map[string]string{"cache": {"cache.demo.doze": "127.0.0.9:1"}}
	d.publishRoutes(nil, &bindPlan{bind: map[string]string{}})
	if _, ok := d.zone.Snapshot()["cache.demo.doze"]; ok {
		t.Fatal("routing a name that was never claimed must not create it")
	}
	if e := other.Snapshot()["aws.demo.doze"]; e.Target != "127.0.0.2:4566" {
		t.Fatalf("the holder's route was disturbed: %+v", e)
	}
}

// When a standalone tool already has a name this stack wanted, the stack still
// starts — and says so against the instance the name was for. A route name
// counts as the instance's: sync-cloud.demo.doze held elsewhere is reported
// against the instance whose listener routes it.
func TestAHeldNameIsReportedAgainstItsInstance(t *testing.T) {
	cfg := &config.Config{StackName: "demo", Defaults: config.Defaults{Domains: true}}
	cfg.Add(&config.InstanceDecl{Type: "aws", Name: "cloud", Port: 24566, Enabled: true})
	cfg.Add(&config.InstanceDecl{Type: "process", Name: "api", Port: 28080, Enabled: true})
	d := &Daemon{cfg: cfg, logf: t.Logf, instRoutes: map[string]map[string]string{"cloud": {"cloud.demo.doze": "127.0.0.22:24566", "sync-cloud.demo.doze": "127.0.0.22:24566"}}}
	d.noteHeld(&names.ErrHeld{Host: "sync-cloud.demo.doze", PID: 412, Owner: "doze-aws"})

	h := &handler{d: d}
	cloud := control.InstanceView{Name: "cloud"}
	api := control.InstanceView{Name: "api"}
	h.hydrateEndpoint(&cloud, endpoints.Endpoint{Name: "cloud"})
	h.hydrateEndpoint(&api, endpoints.Endpoint{Name: "api"})
	if cloud.NameHeld != "sync-cloud.demo.doze is held by doze-aws (pid 412)" {
		t.Fatalf("cloud.NameHeld = %q", cloud.NameHeld)
	}
	if api.NameHeld != "" {
		t.Fatalf("api has nothing to do with that name, got %q", api.NameHeld)
	}
}
