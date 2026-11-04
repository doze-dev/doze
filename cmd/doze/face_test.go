package main

import "testing"

func TestFace(t *testing.T) {
	mac := map[string]string{"HOME": "/Users/x"}
	macSSH := map[string]string{"SSH_CONNECTION": "1.2.3.4 22 5.6.7.8 22"}
	linuxX := map[string]string{"DISPLAY": ":0"}
	linuxWL := map[string]string{"WAYLAND_DISPLAY": "wayland-0"}
	linuxNone := map[string]string{}
	noGUI := map[string]string{"DISPLAY": ":0", "DOZE_NO_GUI": "1"}
	bundle := "/Applications/Doze.app/Contents/MacOS/doze"
	cases := []struct {
		name string
		in   faceInput
		want Face
	}{
		{"daemon", faceInput{Args: []string{"__daemon", "x"}, Env: mac, GOOS: "darwin"}, faceDaemon},
		{"subcommand is the CLI", faceInput{Args: []string{"up"}, Env: mac, GOOS: "darwin"}, faceCLI},
		{"subcommand is the CLI even in a bundle", faceInput{Args: []string{"status"}, Env: mac, Exe: bundle, GOOS: "darwin"}, faceCLI},
		{"open on a mac", faceInput{Args: []string{"open"}, Env: mac, GOOS: "darwin"}, faceApp},
		{"open over ssh", faceInput{Args: []string{"open"}, Env: macSSH, GOOS: "darwin"}, faceStatus},
		{"open on linux with X11", faceInput{Args: []string{"open"}, Env: linuxX, GOOS: "linux"}, faceApp},
		{"open on linux with wayland", faceInput{Args: []string{"open"}, Env: linuxWL, GOOS: "linux"}, faceApp},
		{"open on a linux server", faceInput{Args: []string{"open"}, Env: linuxNone, GOOS: "linux"}, faceStatus},
		{"open with DOZE_NO_GUI", faceInput{Args: []string{"open"}, Env: noGUI, GOOS: "linux"}, faceStatus},
		{"bundle launch", faceInput{Env: mac, Exe: bundle, GOOS: "darwin"}, faceApp},
		{"bundle launch over ssh", faceInput{Env: macSSH, Exe: bundle, GOOS: "darwin"}, faceStatus},
		{"bare doze before the flip", faceInput{Env: mac, StdinTTY: true, StdoutTTY: true, GOOS: "darwin"}, faceCLI},
		{"bare doze after the flip", faceInput{Env: mac, StdinTTY: true, StdoutTTY: true, GOOS: "darwin", BareOpensApp: true}, faceApp},
		{"bare doze after the flip, no display", faceInput{Env: linuxNone, GOOS: "linux", BareOpensApp: true}, faceCLI},
	}
	for _, c := range cases {
		if got := face(c.in); got != c.want {
			t.Errorf("%s: face = %v, want %v", c.name, got, c.want)
		}
	}
}
