//go:build !linux

package supervisor

import "syscall"

// sysProcAttr puts the backend in its own process group. Parent-death
// signalling is Linux-only; on other platforms the daemon reaps backends
// explicitly on shutdown.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
