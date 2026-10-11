package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// session is a program running in a pseudo-terminal, with a terminal emulator
// keeping its screen. The program cannot tell it from a person's terminal: it
// sees a tty, asks it questions, and gets answers.
type session struct {
	cmd  *exec.Cmd
	pty  *os.File
	emu  *vt.Emulator
	mu   sync.Mutex
	last time.Time // when it last wrote
	seen bool      // whether it has written at all
}

func start(bin string, args, env []string, dir string, cols, rows int) (*session, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	var base []string
	for _, e := range os.Environ() {
		// the person's own settings are not the scene's
		switch {
		case hasPrefix(e, "NO_COLOR="), hasPrefix(e, "DOZE_HOME="), hasPrefix(e, "DOZE_ZONE="), hasPrefix(e, "TERM="), hasPrefix(e, "COLORTERM="):
		default:
			base = append(base, e)
		}
	}
	cmd.Env = append(base, "TERM=xterm-256color", "COLORTERM=truecolor")
	cmd.Env = append(cmd.Env, env...)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return nil, fmt.Errorf("starting %s in a terminal: %w", bin, err)
	}
	s := &session{cmd: cmd, pty: f, emu: vt.NewEmulator(cols, rows)}
	go func() { // the program's output is the terminal's input
		buf := make([]byte, 32<<10)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				s.mu.Lock()
				_, _ = s.emu.Write(buf[:n])
				s.last, s.seen = time.Now(), true
				s.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { // and what the terminal answers goes back to the program
		_, _ = io.Copy(f, s.emu)
	}()
	return s, nil
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

// settle waits until the program has written something and then been quiet, or
// until max has passed.
func (s *session) settle(max time.Duration) {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		quiet := s.seen && time.Since(s.last) > 150*time.Millisecond
		s.mu.Unlock()
		if quiet {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

var keyBytes = map[string]string{
	"down": "\x1b[B", "up": "\x1b[A", "right": "\x1b[C", "left": "\x1b[D",
	"enter": "\r", "esc": "\x1b", "tab": "\t", "backspace": "\x7f", "space": " ",
}

// press sends a key, or a piece of text, and waits for the screen to settle.
func (s *session) press(k string) {
	b, ok := keyBytes[k]
	if !ok {
		b = k
	}
	_, _ = s.pty.Write([]byte(b))
	time.Sleep(60 * time.Millisecond)
	s.settle(1500 * time.Millisecond)
}

func (s *session) close() {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_, _ = s.cmd.Process.Wait()
	_ = s.pty.Close()
}
