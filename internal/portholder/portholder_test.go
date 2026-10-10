package portholder

import (
	"net"
	"os"
	"os/exec"
	"testing"
)

func TestParse(t *testing.T) {
	h, ok := parse([]byte("p412\ncpostgres\nn127.0.0.1:5432\nn[::1]:5432\np500\ncother\nn*:5432\n"))
	if !ok || h.PID != 412 || h.Command != "postgres" || h.Addr != "127.0.0.1:5432" {
		t.Fatalf("holder = %+v, %v", h, ok)
	}
	if _, ok := parse(nil); ok {
		t.Fatal("no output is no holder")
	}
}

// Who finds this very process on a port it listens on.
func TestWhoFindsTheListener(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof is not installed")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	h, ok := Who(port)
	if !ok || h.PID != os.Getpid() {
		t.Fatalf("who holds %d = %+v, %v; want pid %d", port, h, ok, os.Getpid())
	}
	ln.Close()
	if _, ok := Who(port); ok {
		t.Fatal("a closed port has no holder")
	}
}
