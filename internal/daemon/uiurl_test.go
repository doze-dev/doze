package daemon

import (
	"testing"

	"github.com/zclconf/go-cty/cty"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/config"
)

// withUI is a driver that, like temporal, exposes its web UI as ui_url.
type withUI struct{ engine.Driver }

func (withUI) Attributes(_ engine.Instance, _ engine.Endpoint) map[string]cty.Value {
	return map[string]cty.Value{"ui_url": cty.StringVal("http://127.0.0.1:8233")}
}

// withoutUI exposes attributes, but no ui_url.
type withoutUI struct{ engine.Driver }

func (withoutUI) Attributes(_ engine.Instance, _ engine.Endpoint) map[string]cty.Value {
	return map[string]cty.Value{"namespace": cty.StringVal("default")}
}

func TestAModuleThatExposesItsUIIsOpenable(t *testing.T) {
	decl := &config.InstanceDecl{Name: "flows", Type: "temporal"}
	if got := uiURL(withUI{}, decl, "127.0.0.1:7233"); got != "http://127.0.0.1:8233" {
		t.Errorf("uiURL = %q, want the module's ui_url", got)
	}
	if got := uiURL(withoutUI{}, decl, "127.0.0.1:5432"); got != "" {
		t.Errorf("uiURL for a module with no ui_url = %q, want none", got)
	}
	if got := uiURL(struct{ engine.Driver }{}, decl, "127.0.0.1:5432"); got != "" {
		t.Errorf("uiURL for a module with no attributes = %q, want none", got)
	}
}

func TestAPublishedLayoutReachesTheDash(t *testing.T) {
	in := engine.ConsoleSpec{
		Panels: []engine.Panel{{ID: "sessions", Kind: "table"}},
		Tabs: []engine.Tab{{ID: "activity", Title: "activity", Body: engine.Node{Kind: "row", Children: []engine.Node{
			{Kind: "table", Panel: "sessions", Weight: 2},
			{Kind: "detail", Panel: "sessions", From: "sessions"},
		}}}},
		Extend: []engine.Extension{{After: "vitals", Node: engine.Node{Kind: "table", Panel: "databases"}}},
	}
	out := specView(in)
	if len(out.Tabs) != 1 || out.Tabs[0].ID != "activity" || len(out.Tabs[0].Body.Children) != 2 ||
		out.Tabs[0].Body.Children[0].Weight != 2 || out.Tabs[0].Body.Children[1].From != "sessions" {
		t.Errorf("tabs = %+v", out.Tabs)
	}
	if len(out.Extend) != 1 || out.Extend[0].After != "vitals" || out.Extend[0].Node.Panel != "databases" {
		t.Errorf("extend = %+v", out.Extend)
	}
}
