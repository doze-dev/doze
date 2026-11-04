package display

import "github.com/doze-dev/doze/internal/control"

// The tabs an instance opens with, by kind (ViewMap.dc.html): every kind
// opens in the same frame and only the front tab differs. The dash and the
// app show the same tabs in the same order because both take them from here.

// Tab is one tab of an instance's page: its id, which the keys and the
// console spec refer to, and the title drawn.
type Tab struct{ ID, Title string }

// The tabs doze itself supplies. A module's console adds its own, with ids
// of the form "console:<id>".
const (
	TabLogs     = "logs"
	TabOverview = "overview"
	TabLastRun  = "last run"
	TabWeb      = "web console"
)

// HasWeb says the instance has a console of its own to open in a browser.
func HasWeb(v control.InstanceView) bool { return v.Console != "" || v.WebURL != "" }

// WebURL is the address of that console: the module's, else the one the
// service was told about.
func WebURL(v control.InstanceView) string {
	if v.Console != "" {
		return v.Console
	}
	return v.WebURL
}

// Tabs is what the instance opens on first, then the rest, by kind. spec is
// the console the module described, if it has been received; nil before
// then, when a console still gets one tab called "console".
func Tabs(v control.InstanceView, spec *control.ConsoleSpec) []Tab {
	if v.HasConsole && v.Kind != "task" {
		out := ConsoleTabs(spec)
		if v.ConsoleFront != "console" && v.ConsoleFront != "" {
			out = append([]Tab{{TabOverview, "overview"}}, out...)
		}
		return append(out, Tab{TabLogs, "logs"})
	}
	switch {
	case v.Kind == "task":
		return []Tab{{TabLastRun, "last run"}}
	case HasWeb(v):
		// ViewMap.dc.html: a module that ships a good UI (temporal) and a service
		// that was told where its own is both open on it.
		return []Tab{{TabWeb, "web console"}, {TabLogs, "logs"}}
	case v.Kind == "module" || v.Kind == "":
		return []Tab{{TabOverview, "overview"}, {TabLogs, "logs"}}
	}
	return []Tab{{TabLogs, "logs"}}
}

// ConsoleTabs are the tabs a console contributes, from its spec: each Tab,
// or one called "console" for a spec of panels with none.
func ConsoleTabs(spec *control.ConsoleSpec) []Tab {
	if spec == nil {
		return []Tab{{"console:_", "console"}}
	}
	var out []Tab
	for _, t := range spec.Tabs {
		out = append(out, Tab{"console:" + t.ID, t.Title})
	}
	if len(out) == 0 && len(spec.Panels) > 0 {
		out = append(out, Tab{"console:_", "console"})
	}
	return out
}
