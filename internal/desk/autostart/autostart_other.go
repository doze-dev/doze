//go:build !darwin && !linux

package autostart

func path() (string, error) { return "", ErrUnsupported }

func entry(string) string { return "" }
