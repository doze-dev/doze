package dash

import "github.com/charmbracelet/x/ansi"

func ansiCutImpl(s string, a, b int) string { return ansi.Cut(s, a, b) }
