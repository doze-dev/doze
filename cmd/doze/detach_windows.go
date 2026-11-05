//go:build windows

package main

import "syscall"

// detachAttr starts the child without a console of its own.
func detachAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{} }
