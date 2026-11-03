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
	err := degradedStatusJSON(decls, views, true, fault.With(fault.Invalid, errors.New("doze.hcl:4: Unsupported argument")))
	if codeOf(err) != 5 {
		t.Fatalf("err = %v (code %d), want exit code 5", err, codeOf(err))
	}
	var buf bytes.Buffer
	ranCommand = "doze status"
	writeEnvelope(&buf, err)
	var env struct {
		OK    bool       `json:"ok"`
		Data  statusJSON `json:"data"`
		Error errorBody  `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, buf.String())
	}
	got := env.Data
	if env.OK || env.Error.Code != fault.Invalid || env.Error.Exit != 5 || !got.Daemon.Running || len(got.Services) != 2 {
		t.Fatalf("got %+v", got)
	}
	if db := got.Services[0]; db.State != "active" || db.Conns != 2 {
		t.Fatalf("db = %+v, want live state", db)
	}
	if api := got.Services[1]; api.State != "asleep" {
		t.Fatalf("api = %+v, want asleep", api)
	}
}
