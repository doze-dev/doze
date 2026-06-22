package supervisor

import "syscall"

// sysProcAttr puts the backend in its own process group and asks the kernel to
// send it SIGINT (a clean "fast" Postgres shutdown) if the daemon dies, so a
// daemon crash does not orphan the backend.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGINT,
	}
}
