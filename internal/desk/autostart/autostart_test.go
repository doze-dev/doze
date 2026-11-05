//go:build darwin || linux

package autostart

import (
	"os"
	"strings"
	"testing"
)

func TestSetWritesAndRemovesTheLoginItem(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	if Enabled() {
		t.Fatal("enabled before anything was written")
	}
	exe := "/Applications/Doze & Co.app/Contents/MacOS/doze"
	if err := Set(true, exe); err != nil {
		t.Fatal(err)
	}
	if !Enabled() {
		t.Fatal("not enabled after Set(true)")
	}
	p, _ := path()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "open") || !strings.Contains(string(b), "Doze") {
		t.Errorf("entry = %s", b)
	}
	if strings.Contains(string(b), "Doze & Co") && !strings.Contains(string(b), "&amp;") && strings.HasSuffix(p, ".plist") {
		t.Error("the plist did not escape &")
	}
	if err := Set(false, exe); err != nil {
		t.Fatal(err)
	}
	if Enabled() {
		t.Fatal("still enabled after Set(false)")
	}
	if err := Set(false, exe); err != nil {
		t.Fatalf("removing twice: %v", err)
	}
}
