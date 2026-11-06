package portholder

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestDescendants(t *testing.T) {
	ps := []byte("  1 0\n 10 1\n 11 10\n 12 11\n 13 10\n 20 1\n bad line\n")
	got := descendants(10, ps)
	slices.Sort(got)
	if want := []int{10, 11, 12, 13}; !slices.Equal(got, want) {
		t.Fatalf("descendants(10) = %v, want %v", got, want)
	}
	if got := descendants(99, ps); !slices.Equal(got, []int{99}) {
		t.Fatalf("an unknown root is just itself, got %v", got)
	}
}

func TestLsofPorts(t *testing.T) {
	out := []byte("p412\nf20\nn127.0.0.1:5173\nf21\nn[::1]:5173\np413\nf3\nn*:24678\n")
	if got := lsofPorts(out); !slices.Equal(got, []int{5173, 24678}) {
		t.Fatalf("ports = %v", got)
	}
	if got := lsofPorts(nil); got != nil {
		t.Fatalf("no output is no ports, got %v", got)
	}
}

func TestSSPorts(t *testing.T) {
	out := []byte(`LISTEN 0 511 127.0.0.1:5173 0.0.0.0:* users:(("node",pid=12,fd=20))
LISTEN 0 4096 [::]:22 [::]:* users:(("sshd",pid=1,fd=3))
LISTEN 0 511 *:24678 *:* users:(("node",pid=13,fd=21),("node",pid=99,fd=4))
`)
	if got := ssPorts(out, []int{12, 13}); !slices.Equal(got, []int{5173, 24678}) {
		t.Fatalf("ports = %v", got)
	}
}

// A child that listens is found from its parent's pid: the case of an app
// started through `sh -c` whose real server is a grandchild.
func TestListeningPortsFindsAChild(t *testing.T) {
	if os.Getenv("PORTHOLDER_LISTEN_HELPER") == "1" {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			os.Exit(2)
		}
		fmt.Println(ln.Addr().(*net.TCPAddr).Port)
		_, _ = io.Copy(io.Discard, os.Stdin) // until the parent closes stdin
		os.Exit(0)
	}
	_, lsofErr := exec.LookPath("lsof")
	_, ssErr := exec.LookPath("ss")
	if lsofErr != nil && ssErr != nil {
		t.Skip("neither lsof nor ss is installed")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestListeningPortsFindsAChild$")
	cmd.Env = append(os.Environ(), "PORTHOLDER_LISTEN_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close(); _ = cmd.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("helper said nothing: %v", err)
	}
	port, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("helper said %q", line)
	}
	if got := ListeningPorts(os.Getpid()); !slices.Contains(got, port) {
		t.Fatalf("ListeningPorts(self) = %v, want it to hold the child's %d", got, port)
	}
	if got := ListeningPorts(0); got != nil {
		t.Fatalf("no pid is no ports, got %v", got)
	}
}
