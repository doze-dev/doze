package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/fault"
)

// With a broken doze.hcl, status --json still gives a script JSON: the blocks
// it could read, live state from a running daemon, the error, and exit 1.
func TestStatusJSONWithABrokenConfig(t *testing.T) {
	decls := []config.ShallowDecl{
		{Kind: config.KindModule, Type: "postgres", Name: "db", Version: "17"},
		{Kind: config.KindApp, Type: config.AppEngine, Name: "api"},
	}
	views := map[string]control.InstanceView{"db": {Name: "db", State: "active", Conns: 2}}
	var buf bytes.Buffer
	err := degradedStatusJSON(&buf, decls, views, true, fault.With(fault.Invalid, errors.New("doze.hcl:4: Unsupported argument")))

	if codeOf(err) != 5 || !errors.As(err, new(shownError)) {
		t.Fatalf("err = %v (code %d), want exit code 5, already shown", err, codeOf(err))
	}
	var got statusJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, buf.String())
	}
	if got.Error == "" || !got.Daemon.Running || len(got.Services) != 2 {
		t.Fatalf("got %+v", got)
	}
	if db := got.Services[0]; db.State != "active" || db.Conns != 2 {
		t.Fatalf("db = %+v, want live state", db)
	}
	if api := got.Services[1]; api.State != "asleep" {
		t.Fatalf("api = %+v, want asleep", api)
	}
}
