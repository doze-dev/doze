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
// holds :80 reads.
func TestRoutesArePublishedInTheSharedRegistry(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{StackName: "demo", Home: home, Defaults: config.Defaults{Domains: true}}
	cfg.Add(&config.InstanceDecl{Type: "process", Name: "api", Port: 28080, Enabled: true, Spec: appSpec(t, 28080)})
	cfg.Add(&config.InstanceDecl{Type: config.AWSUnifiedType, Name: "cloud", Port: 24566, Enabled: true})
	zone := names.Open(home, "doze")
	d := &Daemon{cfg: cfg, logf: t.Logf, zone: zone, resources: map[string]string{}}

	plan := &bindPlan{
		bind:    map[string]string{"api": "127.0.0.21:28080", "cloud": "127.0.0.22:24566"},
		resolve: map[string]net.IP{},
	}
	eps := []endpoints.Endpoint{{Name: "api", Engine: "process", Address: "127.0.0.1:28080", Domain: "api.demo.doze"}}
	aws := d.buildAWSRoutes(plan)
	if aws["aws.demo.doze"] != "127.0.0.22:24566" {
		t.Fatalf("aws route = %v", aws)
	}
	if ip := plan.resolve["aws.demo.doze"]; ip == nil || ip.String() != "127.0.0.1" {
		t.Fatalf("the aws host must resolve to the front door, got %v", ip)
	}

	// The names are claimed first, as the daemon does, then routed.
	for host, ip := range map[string]string{"api.demo.doze": "127.0.0.21", "aws.demo.doze": "127.0.0.1"} {
		if _, err := zone.ClaimAt(names.Name{Host: host, Tier: names.TierQualified}, net.ParseIP(ip)); err != nil {
			t.Fatal(err)
		}
	}
	d.publishRoutes(eps, plan, aws)

	got := zone.Snapshot()
	// The app's route is its public address — doze's own listener — so a request
	// through the front door wakes it like any other connection.
	if e := got["api.demo.doze"]; e.Target != "127.0.0.21:28080" {
		t.Fatalf("api route = %+v", e)
	}
	if e := got["aws.demo.doze"]; e.Target != "127.0.0.22:24566" {
		t.Fatalf("aws route = %+v", e)
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
	d.publishRoutes(nil, &bindPlan{bind: map[string]string{}}, map[string]string{"cache.demo.doze": "127.0.0.9:1"})
	if _, ok := d.zone.Snapshot()["cache.demo.doze"]; ok {
		t.Fatal("routing a name that was never claimed must not create it")
	}
	if e := other.Snapshot()["aws.demo.doze"]; e.Target != "127.0.0.2:4566" {
		t.Fatalf("the holder's route was disturbed: %+v", e)
	}
}

// When a standalone tool already has a name this stack wanted, the stack still
// starts — and says so against the instance the name was for.
func TestAHeldNameIsReportedAgainstItsInstance(t *testing.T) {
	cfg := &config.Config{StackName: "demo", Defaults: config.Defaults{Domains: true}}
	cfg.Add(&config.InstanceDecl{Type: config.AWSUnifiedType, Name: "cloud", Port: 24566, Enabled: true})
	cfg.Add(&config.InstanceDecl{Type: "process", Name: "api", Port: 28080, Enabled: true})
	d := &Daemon{cfg: cfg, logf: t.Logf}
	d.noteHeld(&names.ErrHeld{Host: "aws.demo.doze", PID: 412, Owner: "doze-aws"})

	h := &handler{d: d}
	cloud := control.InstanceView{Name: "cloud"}
	api := control.InstanceView{Name: "api"}
	h.hydrateEndpoint(&cloud, endpoints.Endpoint{Name: "cloud"})
	h.hydrateEndpoint(&api, endpoints.Endpoint{Name: "api"})
	if cloud.NameHeld != "aws.demo.doze is held by doze-aws (pid 412)" {
		t.Fatalf("cloud.NameHeld = %q", cloud.NameHeld)
	}
	if api.NameHeld != "" {
		t.Fatalf("api has nothing to do with the aws host, got %q", api.NameHeld)
	}
}
