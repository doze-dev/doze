package serve

import (
	"net"
	"os"
	"testing"
)

func TestPortInUseReadsTheWordsOfSeveralRuntimes(t *testing.T) {
	for in, want := range map[string]int{
		"listen tcp 127.0.0.1:3000: bind: address already in use":     3000,
		"Error: listen EADDRINUSE: address already in use :::8090":    8090,
		"OSError: [Errno 48] Address already in use (127.0.0.1:5000)": 5000,
		"panic: STRIPE_KEY is required":                               0,
		"dial tcp 127.0.0.1:5432: connect: connection refused":        0,
	} {
		if got := portInUse(in); got != want {
			t.Errorf("portInUse(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestPortHolderFindsTheListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	h := portHolder(port)
	if h == nil {
		t.Skip("neither lsof nor ss here")
	}
	if h.PID != os.Getpid() || h.Port != port {
		t.Errorf("holder = %+v, want pid %d", h, os.Getpid())
	}
}
