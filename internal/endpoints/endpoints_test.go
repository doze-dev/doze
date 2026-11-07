package endpoints

import (
	"context"
	"reflect"
	"testing"

	"github.com/doze-dev/doze-sdk/engine"

	"github.com/doze-dev/doze/internal/config"
)

// A module of the old shape: one port, a URL built from the address it is given.
type plainDriver struct{}

func (plainDriver) Type() string { return "plaindb" }
func (plainDriver) Resolve(context.Context, engine.VersionSpec, engine.Platform, engine.Locker, engine.Fetcher) (engine.Toolchain, error) {
	return engine.Toolchain{}, nil
}
func (plainDriver) Provision(context.Context, engine.Instance, engine.Toolchain) error { return nil }
func (plainDriver) Provisioned(string) bool                                            { return true }
func (plainDriver) BackendSocket(dir string, _ int) string                             { return dir + "/db.sock" }
func (plainDriver) ConnString(_ engine.Instance, ep engine.Endpoint) (string, string) {
	return "DATABASE_URL", "plain://" + ep.TCPAddr + "/app"
}

// A module that declares listeners and builds its URL from the grant.
type netDriver struct{ plainDriver }

func (netDriver) Type() string { return "netbroker" }
func (netDriver) Network(engine.Instance) engine.Network {
	return engine.Network{Listeners: []engine.Listener{
		{Name: engine.MainListener, Protocol: engine.ProtocolTCP},
		{Name: "console", Port: 9093, Protocol: engine.ProtocolHTTP, Routes: []string{"@"}},
	}}
}
func (netDriver) ConnString(inst engine.Instance, ep engine.Endpoint) (string, string) {
	if a, ok := inst.Grant.Addr(engine.MainListener); ok {
		return "BROKERS", a
	}
	return "BROKERS", ep.TCPAddr
}

func init() {
	engine.Register(plainDriver{})
	engine.Register(netDriver{})
}

func stack(domains bool) *config.Config {
	cfg := &config.Config{StackName: "shop", Defaults: config.Defaults{Domains: domains}}
	cfg.Add(&config.InstanceDecl{Type: "plaindb", Name: "db", Port: 5432, Enabled: true})
	cfg.Add(&config.InstanceDecl{Type: "netbroker", Name: "events", Port: 9092, Enabled: true})
	return cfg
}

// What a module of the old shape gets is what it got before listeners: the
// address, the name put into its URL, nothing else.
func TestAPlainModulesEndpointIsUnchanged(t *testing.T) {
	eps, err := For(stack(true))
	if err != nil {
		t.Fatal(err)
	}
	want := Endpoint{Name: "db", Engine: "plaindb", Address: "127.0.0.1:5432", Domain: "db.shop.doze", EnvVar: "DATABASE_URL", URL: "plain://db.shop.doze:5432/app"}
	if !reflect.DeepEqual(eps[0], want) {
		t.Fatalf("db = %+v\nwant %+v", eps[0], want)
	}
	off, _ := For(stack(false))
	if off[0].URL != "plain://127.0.0.1:5432/app" || off[0].Domain != "" {
		t.Errorf("without names db = %+v", off[0])
	}
}

// A module that declares listeners builds its URL from the grant itself, and
// the manifest lists each listener where it is reached.
func TestANetworkedModulesEndpointComesFromItsGrant(t *testing.T) {
	eps, err := For(stack(true))
	if err != nil {
		t.Fatal(err)
	}
	ev := eps[1]
	if ev.URL != "events.shop.doze:9092" || ev.Domain != "events.shop.doze" || ev.EnvVar != "BROKERS" {
		t.Errorf("events = %+v", ev)
	}
	want := []Listener{
		{Name: "main", Address: "events.shop.doze:9092"},
		{Name: "console", Address: "events.shop.doze:9093", URL: "http://events.shop.doze", Hosts: []string{"events.shop.doze"}},
	}
	if !reflect.DeepEqual(ev.Listeners, want) {
		t.Errorf("listeners = %+v\nwant %+v", ev.Listeners, want)
	}
	off, _ := For(stack(false))
	if off[1].URL != "127.0.0.1:9092" || off[1].Listeners[1].URL != "http://127.0.0.1:9093" {
		t.Errorf("without names events = %+v", off[1])
	}
}
