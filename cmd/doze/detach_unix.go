//go:build !windows

package main

import "syscall"

// detachAttr puts a child in its own session, so closing the terminal that
// started it does not take it down.
func detachAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
