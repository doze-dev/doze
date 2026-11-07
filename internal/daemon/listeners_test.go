package daemon

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	names "github.com/doze-dev/doze-names"

	"github.com/doze-dev/doze-sdk/engine"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/endpoints"
	"github.com/doze-dev/doze/internal/proxy"
)

// netSpec is the fake module's config: the ports of its two extra listeners.
type netSpec struct{ Console, Registry int }

// netModule declares listeners like the kafka module: a tcp main, a console on
// the instance's name, and a registry on a child, with ports from its config so
// a test can give it free ones.
type netModule struct{ quietDriver }

func (netModule) Type() string { return "tnet" }
func (netModule) Network(inst engine.Instance) engine.Network {
	s, _ := inst.Spec.(netSpec)
	return engine.Network{Listeners: []engine.Listener{
		{Name: engine.MainListener, Protocol: engine.ProtocolTCP},
		{Name: "console", Port: s.Console, Protocol: engine.ProtocolHTTP, Routes: []string{"@"}},
		{Name: "registry", Port: s.Registry, Protocol: engine.ProtocolHTTP, Routes: []string{"registry"}},
	}}
}

func init() { engine.Register(netModule{}); engine.Register(quietDriver{}) }

func netStack(t *testing.T, console, registry int) *config.Config {
	t.Helper()
	// A short data folder: a unix socket path has a limit (104 bytes on macOS),
	// which a test's own temporary folder can pass on its own.
	data, err := os.MkdirTemp("", "dz")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(data) })
	cfg := &config.Config{StackName: "shop", Home: t.TempDir(), DataDir: data, Defaults: config.Defaults{Domains: true}}
	cfg.Add(&config.InstanceDecl{Type: "tnet", Name: "events", Port: 9092, Enabled: true, Spec: netSpec{Console: console, Registry: registry}})
	return cfg
}

// Every listener of an instance is on the main listener's address, at its own
// port, with the names its routes answer for.
func TestAnInstancesListenersArePlacedOnItsAddress(t *testing.T) {
	cfg := netStack(t, 9093, 8081)
	d := &Daemon{cfg: cfg, logf: t.Logf}
	got, err := d.listenersOf("events", "127.0.0.31:9092")
	if err != nil {
		t.Fatal(err)
	}
	sock := cfg.SocketDir("events")
	want := []listenerBind{
		{Name: "main", Public: "127.0.0.31:9092"},
		{Name: "console", Public: "127.0.0.31:9093", Backend: filepath.Join(sock, "console.sock"), Hosts: []string{"events.shop.doze"}},
		{Name: "registry", Public: "127.0.0.31:8081", Backend: filepath.Join(sock, "registry.sock"), Hosts: []string{"registry.events.shop.doze"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listeners =\n%+v\nwant\n%+v", got, want)
	}
	// A module of the old shape has its main listener only.
	cfg.Add(&config.InstanceDecl{Type: "tquiet", Name: "db", Port: 5432, Enabled: true})
	if got, _ := d.listenersOf("db", "127.0.0.32:5432"); len(got) != 1 || got[0].Public != "127.0.0.32:5432" || len(got[0].Hosts) != 0 {
		t.Errorf("db = %+v", got)
	}
}

type wakeRouter struct {
	mu    sync.Mutex
	boots int
}

func (r *wakeRouter) Boot(context.Context, string) (engine.Endpoint, error) {
	r.mu.Lock()
	r.boots++
	r.mu.Unlock()
	return engine.Endpoint{}, nil
}
func (*wakeRouter) Acquire(string) {}
func (*wakeRouter) Release(string) {}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// Started, an instance serves each extra listener on its port, forwarding to the
// listener's socket and waking the instance; its route names resolve to its
// address and are routed to the listener; removed, they go with it.
func TestAnInstancesExtraListenersAreServedAndRouted(t *testing.T) {
	console, registry := freePort(t), freePort(t)
	cfg := netStack(t, console, registry)
	sock := cfg.SocketDir("events")
	if err := os.MkdirAll(sock, 0o755); err != nil {
		t.Fatal(err)
	}
	backend, err := net.Listen("unix", filepath.Join(sock, "console.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		for {
			c, err := backend.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				if _, err := http.ReadRequest(bufio.NewReader(c)); err == nil {
					_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 7\r\nConnection: close\r\n\r\nconsole"))
				}
			}()
		}
	}()

	r := &wakeRouter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	zone := names.Open(cfg.Home, "doze")
	d := &Daemon{cfg: cfg, logf: t.Logf, px: proxy.New(r), rootCtx: ctx, instWG: &wg, zone: zone,
		running: map[string]*instHandle{}, nameOnly: map[string]bool{}, consoles: map[string]string{},
		resolveMap: map[string]net.IP{}, plan: &bindPlan{bind: map[string]string{}, resolve: map[string]net.IP{}}}
	main := net.JoinHostPort("127.0.0.1", strconv.Itoa(freePort(t)))
	drv, _ := engine.Lookup("tnet")
	if err := d.startProxyInstance("events", "tnet", "events.shop.doze", main, drv, nil); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(console) + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("console through its listener: %d", resp.StatusCode)
	}
	r.mu.Lock()
	if r.boots == 0 {
		t.Error("a request to the console did not wake the instance")
	}
	r.mu.Unlock()

	// Its names resolve to its address, and each is routed to its listener.
	for _, host := range []string{"events.shop.doze", "registry.events.shop.doze"} {
		if ip := d.resolveMap[host]; ip == nil || ip.String() != "127.0.0.1" {
			t.Errorf("%s resolves to %v", host, ip)
		}
	}
	routes := d.moduleRoutes()
	if routes["events.shop.doze"] != "127.0.0.1:"+strconv.Itoa(console) || routes["registry.events.shop.doze"] != "127.0.0.1:"+strconv.Itoa(registry) {
		t.Errorf("routes = %v", routes)
	}

	// Gone with the instance.
	d.forgetListeners("events")
	if _, ok := d.resolveMap["registry.events.shop.doze"]; ok || len(d.moduleRoutes()) != 0 {
		t.Errorf("after removal: resolve %v, routes %v", d.resolveMap, d.moduleRoutes())
	}
}

// Names on, but no address of its own for each service (the machine is not set
// up): every listener is on 127.0.0.1, so two instances' extra listeners on one
// port collide like two main ports do, and the error says which.
func TestExtraListenersCollideWithoutAddressesOfTheirOwn(t *testing.T) {
	cfg := netStack(t, 9093, 8081)
	cfg.Add(&config.InstanceDecl{Type: "tnet", Name: "audit", Port: 9192, Enabled: true, Spec: netSpec{Console: 9093, Registry: 8181}})
	d := &Daemon{cfg: cfg, logf: t.Logf}
	eps := []endpoints.Endpoint{
		{Name: "events", Engine: "tnet", Address: "127.0.0.1:9092", Domain: "events.shop.doze"},
		{Name: "audit", Engine: "tnet", Address: "127.0.0.1:9192", Domain: "audit.shop.doze"},
	}
	_, err := d.buildBindPlan(eps, nil)
	if err == nil || !strings.Contains(err.Error(), "9093") || !strings.Contains(err.Error(), "console") || !strings.Contains(err.Error(), "audit") {
		t.Fatalf("err = %v, want the two consoles on 9093 named", err)
	}
}
