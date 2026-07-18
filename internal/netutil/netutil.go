// Package netutil holds small shared TCP helpers: kernel-assigned free ports
// and the listener-keeping variant that avoids the pick/bind TOCTOU race.
package netutil

import "net"

// FreePort asks the kernel for an available TCP port on 127.0.0.1 by binding
// port 0 and immediately closing the listener. Inherently racy between the
// close and the caller's own bind; use ListenFree when the caller can keep
// the listener instead.
func FreePort() (int, error) {
	ln, port, err := ListenFree("127.0.0.1")
	if err != nil {
		return 0, err
	}
	_ = ln.Close()
	return port, nil
}

// ListenFree binds a fresh kernel-chosen TCP port on host and returns the live
// listener plus its port — the TOCTOU-free variant for callers that keep (or
// probe with) the listener.
func ListenFree(host string) (net.Listener, int, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return nil, 0, err
	}
	return ln, ln.Addr().(*net.TCPAddr).Port, nil
}
