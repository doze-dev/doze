package display

import (
	"strings"
	"testing"

	"github.com/doze-dev/doze/internal/control"
)

// The held sentence agrees with its subject: one dependant is running, three
// are; one connection is open, two are.
func TestHeldSentenceAgrees(t *testing.T) {
	dep := func(n string) control.HolderView { return control.HolderView{Kind: "dependant", Name: n} }
	conns := func(n int) control.HolderView { return control.HolderView{Kind: "connection", Count: n} }
	for _, c := range []struct {
		holders []control.HolderView
		want    string
	}{
		{[]control.HolderView{dep("api")}, "api is running and needs it."},
		{[]control.HolderView{dep("gateway"), dep("orders"), dep("recommendations")}, "gateway, orders and recommendations are running and need it."},
		{[]control.HolderView{dep("orders"), dep("gateway")}, "orders and gateway are running and need it."},
		{[]control.HolderView{dep("api"), conns(3)}, "api is running and needs it, and 3 connections are open to it right now."},
		{[]control.HolderView{dep("api"), conns(1)}, "api is running and needs it, and 1 connection is open to it right now."},
		{[]control.HolderView{conns(1)}, "1 connection is open to it right now."},
	} {
		var b strings.Builder
		for _, s := range HeldSentence(control.InstanceView{Holders: c.holders}) {
			b.WriteString(s.Text)
		}
		if b.String() != c.want {
			t.Errorf("got %q, want %q", b.String(), c.want)
		}
	}
}
