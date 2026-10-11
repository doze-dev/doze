// Command look checks the dash against its boards, the way a person would: it
// runs the real program in a real pseudo-terminal, presses real keys, reads the
// screen back through a terminal emulator, and draws it beside the board.
//
//	go run ./tools/look                       every scene
//	go run ./tools/look palette crash         some
//	go run ./tools/look -out /tmp/look home   where the images go
//	go run ./tools/look live -doze ./doze -home /tmp/dz-try -dir ./project -keys "down enter"
//
// For a scene it writes <out>/<scene>.png: the board on the left, the dash on
// the right, both drawn by the same browser at the board's size. Read that
// image. A scene is a fixture stack (internal/dash/fixture), the keys pressed,
// and the board it is compared with: see scenes.go.
//
// What it does not check: your terminal's fonts, how it draws unusual glyphs,
// and colour on your display. Those need a person at a real terminal.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "host" {
		host(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "live" {
		live(os.Args[2:])
		return
	}
	fs := flag.NewFlagSet("look", flag.ExitOnError)
	out := fs.String("out", "", "where the images go (default .look at the repository root)")
	_ = fs.Parse(os.Args[1:])

	names := fs.Args()
	if len(names) == 0 {
		for _, s := range scenes {
			names = append(names, s.name)
		}
	}
	dest := *out
	if dest == "" {
		root, err := repoRoot()
		if err != nil {
			fatal(err)
		}
		dest = filepath.Join(root, ".look")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		fatal(err)
	}
	abs, _ := filepath.Abs(dest)
	for _, n := range names {
		s, ok := find(n)
		if !ok {
			fatal(fmt.Errorf("no scene %q; known: %s", n, strings.Join(sceneNames(), ", ")))
		}
		img, err := run(s, abs)
		if err != nil {
			fatal(fmt.Errorf("%s: %w", n, err))
		}
		fmt.Println(img)
	}
}

// live runs a real doze against a real daemon and draws the screen, with no
// board: it is for looking at the dash as it is, not for comparing.
func live(args []string) {
	fs := flag.NewFlagSet("live", flag.ExitOnError)
	bin := fs.String("doze", "doze", "the doze binary")
	home := fs.String("home", "", "DOZE_HOME (an isolated one)")
	dir := fs.String("dir", ".", "the project directory")
	keys := fs.String("keys", "", "keys to press, space separated: down up enter esc tab, or text")
	out := fs.String("out", ".look/live.png", "the image")
	cols := fs.Int("cols", 136, "columns")
	rows := fs.Int("rows", 42, "rows")
	wait := fs.Duration("wait", 2*time.Second, "how long to let it start")
	_ = fs.Parse(args)
	if *home == "" {
		fatal(fmt.Errorf("-home is required: the live dash must never be pointed at your own daemon by accident"))
	}
	abs, _ := filepath.Abs(*bin)
	sc := scene{name: "live", cols: *cols, rows: *rows, wpx: 1200, hpx: 760, keys: strings.Fields(*keys)}
	sess, err := start(abs, nil, []string{"DOZE_HOME=" + *home, "DOZE_ZONE=off"}, *dir, *cols, *rows)
	if err != nil {
		fatal(err)
	}
	defer sess.close()
	sess.settle(*wait)
	for _, k := range sc.keys {
		sess.press(k)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fatal(err)
	}
	if err := drawScreen(sess, *out); err != nil {
		fatal(err)
	}
	fmt.Println(*out)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "look:", err)
	os.Exit(1)
}
