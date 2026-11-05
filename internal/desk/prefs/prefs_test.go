package prefs

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPrefsRoundTripAndForgiveABrokenFile(t *testing.T) {
	home := t.TempDir()
	if p := Load(home); p.Window.Placed() || p.LastDir != "" {
		t.Fatalf("a missing file is empty prefs, got %+v", p)
	}
	want := Prefs{Window: Frame{X: 40, Y: 60, W: 1280, H: 800}, LastDir: "/code/shop"}
	if err := Save(home, want); err != nil {
		t.Fatal(err)
	}
	if got := Load(home); !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(Path(home) + ".tmp"); !os.IsNotExist(err) {
		t.Error("the temporary file was left behind")
	}
	if err := os.WriteFile(Path(home), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := Load(home); p.Window.Placed() {
		t.Fatalf("a broken file is empty prefs, got %+v", p)
	}
	if filepath.Base(filepath.Dir(Path(home))) != "desk" {
		t.Errorf("prefs live under desk/, got %s", Path(home))
	}
}
