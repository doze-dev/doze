package actions

import "testing"

func TestLookupResolvesNamesAndAliases(t *testing.T) {
	cases := map[string]string{
		"wake":      "wake",
		"boot":      "wake",
		"reap":      "sleep",
		"keepawake": "pin",
		"WAKE":      "wake", // case-insensitive
	}
	for in, want := range cases {
		a, ok := Lookup(in)
		if !ok || a.Name != want {
			t.Errorf("Lookup(%q) = (%q, %v), want (%q, true)", in, a.Name, ok, want)
		}
	}
	if _, ok := Lookup("frobnicate"); ok {
		t.Error("Lookup(frobnicate) resolved; want miss")
	}
}

func TestMatchFiltersDashActions(t *testing.T) {
	for _, a := range Match("") {
		if !a.Dash {
			t.Errorf("Match(\"\") returned non-dash action %q", a.Name)
		}
	}
	rs := Match("re")
	names := map[string]bool{}
	for _, a := range rs {
		names[a.Name] = true
	}
	// "re" hits restart and reset directly, and sleep via its "reap" alias.
	for _, want := range []string{"restart", "reset", "sleep"} {
		if !names[want] {
			t.Errorf("Match(\"re\") missing %q (got %v)", want, names)
		}
	}
}

func TestOpActionsDeclareOps(t *testing.T) {
	for _, a := range All() {
		if a.Kind == KindOp && a.Op == "" {
			t.Errorf("action %q is KindOp but declares no control op", a.Name)
		}
		if a.Kind == KindLocal && a.Op != "" {
			t.Errorf("action %q is KindLocal but declares op %q", a.Name, a.Op)
		}
	}
}
