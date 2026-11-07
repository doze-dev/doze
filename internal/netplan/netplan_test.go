package netplan

import (
	"reflect"
	"strings"
	"testing"

	"github.com/doze-dev/doze-sdk/engine"
)

type plain struct{ engine.Driver }

type networked struct {
	engine.Driver
	n engine.Network
}

func (d networked) Network(engine.Instance) engine.Network { return d.n }

var kafka = engine.Network{Listeners: []engine.Listener{
	{Name: engine.MainListener, Protocol: engine.ProtocolTCP},
	{Name: "console", Port: 9093, Protocol: engine.ProtocolHTTP, Routes: []string{"@"}},
	{Name: "registry", Port: 8081, Protocol: engine.ProtocolHTTP, Routes: []string{"registry"}},
}}

func TestAModuleWithoutANetworkHasOneTCPListenerOnItsPort(t *testing.T) {
	n, err := Network(plain{}, engine.Instance{Name: "db"}, 5432)
	if err != nil {
		t.Fatal(err)
	}
	want := engine.Network{Listeners: []engine.Listener{{Name: engine.MainListener, Port: 5432, Protocol: engine.ProtocolTCP}}}
	if !reflect.DeepEqual(n, want) {
		t.Fatalf("network = %+v", n)
	}
	if Declared(plain{}, engine.Instance{}) {
		t.Error("a plain module declares no network")
	}
	// A module that implements the interface but answers with nothing (a plugin
	// that did not advertise it) has the default too.
	if n, _ := Network(networked{}, engine.Instance{}, 5432); n.Listeners[0].Port != 5432 || Declared(networked{}, engine.Instance{}) {
		t.Errorf("an empty answer is the default: %+v", n)
	}
}

func TestTheMainListenerTakesTheDeclaredPort(t *testing.T) {
	n, err := Network(networked{n: kafka}, engine.Instance{Name: "events"}, 19092)
	if err != nil {
		t.Fatal(err)
	}
	if l, _ := n.Listener(engine.MainListener); l.Port != 19092 {
		t.Fatalf("main port = %d, want the declared 19092", l.Port)
	}
	if !Declared(networked{n: kafka}, engine.Instance{}) {
		t.Error("a module with listeners declares a network")
	}
	// The module's answer is not changed in place.
	if kafka.Listeners[0].Port != 0 {
		t.Error("Network wrote into the module's network")
	}
}

func TestANetworkTheHostWillNotTakeIsAnError(t *testing.T) {
	bad := engine.Network{Listeners: []engine.Listener{{Name: engine.MainListener, Protocol: engine.ProtocolTCP, Routes: []string{"@"}}}}
	if _, err := Network(networked{n: bad}, engine.Instance{Name: "x", Type: "thing"}, 1); err == nil || !strings.Contains(err.Error(), "thing") {
		t.Errorf("err = %v, want it to name the module", err)
	}
	// Another listener on the declared port: the person's port wins, and says so.
	if _, err := Network(networked{n: kafka}, engine.Instance{Name: "events", Type: "kafka"}, 9093); err == nil || !strings.Contains(err.Error(), `"console"`) || !strings.Contains(err.Error(), "9093") {
		t.Errorf("err = %v, want the clash named", err)
	}
}

func TestTheGrantWithNames(t *testing.T) {
	n, _ := Network(networked{n: kafka}, engine.Instance{}, 9092)
	g := Grant(n, Place{Host: "events.shop.doze", AddrHost: "127.0.0.1", Front: true, SocketDir: "/s", MainBackend: "/s/kafka.sock"})
	want := engine.Grant{Host: "events.shop.doze", Listeners: map[string]engine.GrantedListener{
		engine.MainListener: {Addr: "events.shop.doze:9092", Backend: "/s/kafka.sock"},
		"console":           {Addr: "events.shop.doze:9093", URL: "http://events.shop.doze", Hosts: []string{"events.shop.doze"}, Backend: "/s/console.sock"},
		"registry":          {Addr: "events.shop.doze:8081", URL: "http://registry.events.shop.doze", Hosts: []string{"registry.events.shop.doze"}, Backend: "/s/registry.sock"},
	}}
	if !reflect.DeepEqual(g, want) {
		t.Fatalf("grant =\n%+v\nwant\n%+v", g, want)
	}
}

func TestTheGrantWithoutNamesOrWithoutTheFrontDoor(t *testing.T) {
	n, _ := Network(networked{n: kafka}, engine.Instance{}, 9092)
	off := Grant(n, Place{AddrHost: "127.0.0.1", Front: true, SocketDir: "/s", MainBackend: "/s/kafka.sock"})
	if off.Named() {
		t.Error("no host, no name")
	}
	if l := off.Listeners["console"]; l.Addr != "127.0.0.1:9093" || l.URL != "http://127.0.0.1:9093" || len(l.Hosts) != 0 || l.Backend != "/s/console.sock" {
		t.Errorf("console without names = %+v", l)
	}
	// front = false: named, reached on the ports, no routes.
	unfronted := Grant(n, Place{Host: "events.shop.doze", AddrHost: "127.0.0.1", SocketDir: "/s", MainBackend: "/s/kafka.sock"})
	if l := unfronted.Listeners["console"]; l.URL != "http://events.shop.doze:9093" || len(l.Hosts) != 0 {
		t.Errorf("console with front = false = %+v", l)
	}
}

func TestRoutesAreTheHostsAnInstanceAnswersFor(t *testing.T) {
	aws := engine.Network{Listeners: []engine.Listener{{Name: engine.MainListener, Protocol: engine.ProtocolHTTP, Routes: []string{"@", "sync-@"}}}}
	n, _ := Network(networked{n: aws}, engine.Instance{}, 4566)
	g := Grant(n, Place{Host: "aws.shop.doze", AddrHost: "127.0.0.1", Front: true, SocketDir: "/s", MainBackend: "/s/aws.sock"})
	got := Routes(g)
	want := []Route{{Host: "aws.shop.doze", Listener: engine.MainListener}, {Host: "sync-aws.shop.doze", Listener: engine.MainListener}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routes = %+v", got)
	}
}
