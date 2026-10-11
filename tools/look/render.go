package main

import (
	"fmt"
	"html"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

const (
	defaultFg = "#cfcbe3"
	defaultBg = "#15131d"
)

func chromePath() string {
	if p := os.Getenv("CHROME"); p != "" {
		return p
	}
	return "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
}

// repoRoot is the directory with the boards in it, found from where we run.
func repoRoot() (string, error) {
	d, _ := os.Getwd()
	for {
		if st, err := os.Stat(filepath.Join(d, "docs/design/next/boards")); err == nil && st.IsDir() {
			return d, nil
		}
		p := filepath.Dir(d)
		if p == d {
			return "", fmt.Errorf("docs/design/next/boards not found above %s", d)
		}
		d = p
	}
}

func chrome(url, png string, w, h int) error {
	cmd := exec.Command(chromePath(), "--headless=new", "--disable-gpu", "--hide-scrollbars",
		"--force-device-scale-factor=1", "--virtual-time-budget=4000",
		fmt.Sprintf("--window-size=%d,%d", w, h), "--screenshot="+png, url)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("chrome: %w\n%s", err, out)
	}
	if _, err := os.Stat(png); err != nil {
		return fmt.Errorf("chrome wrote no image")
	}
	return nil
}

func hex(c color.Color, def string) string {
	if c == nil {
		return def
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// screenHTML draws the emulator's screen as a grid: one cell is one character
// wide whatever the glyph, as in a terminal.
func screenHTML(s *session) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	w, h := s.emu.Width(), s.emu.Height()
	for y := 0; y < h; y++ {
		var run strings.Builder
		var cur string
		flush := func() {
			if run.Len() > 0 {
				fmt.Fprintf(&b, `<span style="%s">%s</span>`, cur, run.String())
				run.Reset()
			}
		}
		for x := 0; x < w; x++ {
			c := s.emu.CellAt(x, y)
			content, st := " ", uv.Style{}
			if c != nil {
				st = c.Style
				if c.Content != "" {
					content = c.Content
				}
				if c.Width == 0 {
					continue
				}
			}
			fg, bg := hex(st.Fg, defaultFg), ""
			if st.Bg != nil {
				bg = hex(st.Bg, "")
			}
			if st.Attrs&uv.AttrReverse != 0 {
				if bg == "" {
					bg = defaultFg
				}
				fg, bg = defaultBg, hex(st.Fg, defaultFg)
			}
			css := "color:" + fg
			if bg != "" {
				css += ";background:" + bg
			}
			if st.Attrs&uv.AttrBold != 0 {
				css += ";font-weight:700"
			}
			if st.Attrs&uv.AttrFaint != 0 {
				css += ";opacity:.6"
			}
			if st.Attrs&uv.AttrItalic != 0 {
				css += ";font-style:italic"
			}
			if st.Underline != 0 {
				css += ";text-decoration:underline"
			}
			if css != cur {
				flush()
				cur = css
			}
			r := []rune(content)
			if len(r) == 1 && r[0] < 0x7f {
				run.WriteString(html.EscapeString(content))
			} else {
				fmt.Fprintf(&run, `<span style="display:inline-block;width:%dch;overflow:visible;white-space:pre">%s</span>`, max(c.Width, 1), html.EscapeString(content))
			}
		}
		flush()
		b.WriteString("\n")
	}
	return b.String()
}

func screenPage(body string, wpx, hpx int) string {
	return fmt.Sprintf(`<!doctype html><meta charset="utf-8"><style>
html,body{margin:0;background:%[3]s}
pre{margin:0;box-sizing:border-box;width:%[1]dpx;height:%[2]dpx;padding:18px 22px;background:%[3]s;color:%[4]s;
font:400 14px/17px 'JetBrains Mono',Menlo,Monaco,monospace;font-variant-ligatures:none;overflow:hidden}
</style><link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;700&display=swap" rel="stylesheet">
<pre>%[5]s</pre>`, wpx, hpx, defaultBg, defaultFg, body)
}

// drawScreen writes what the session's terminal shows as an image.
func drawScreen(s *session, out string, wpx, hpx int) error {
	dir, err := os.MkdirTemp("", "doze-look-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	page := filepath.Join(dir, "screen.html")
	if err := os.WriteFile(page, []byte(screenPage(screenHTML(s), wpx, hpx)), 0o644); err != nil {
		return err
	}
	abs, _ := filepath.Abs(out)
	return chrome("file://"+page, abs, wpx, hpx)
}

// run plays a scene and writes its three images: the board, the dash, and
// both together. It returns the last.
func run(sc scene, outDir string) (string, error) {
	root, err := repoRoot()
	if err != nil {
		return "", err
	}
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	sess, err := start(self, []string{"host", sc.fixture}, nil, root, sc.cols, sc.rows)
	if err != nil {
		return "", err
	}
	defer sess.close()
	sess.settle(5 * time.Second)
	for _, k := range sc.keys {
		sess.press(k)
	}
	// the live streams (stats, journal, logs) arrive after the first frame
	time.Sleep(300 * time.Millisecond)
	sess.settle(2 * time.Second)

	dashPNG := filepath.Join(outDir, sc.name+".dash.png")
	boardPNG := filepath.Join(outDir, sc.name+".board.png")
	if err := drawScreen(sess, dashPNG, sc.wpx, sc.hpx); err != nil {
		return "", err
	}
	board := filepath.Join(root, "docs/design/next/boards", sc.board+".dc.html")
	if err := chrome("file://"+board, boardPNG, sc.wpx, sc.hpx); err != nil {
		return "", err
	}
	pair := filepath.Join(outDir, sc.name+".html")
	body := fmt.Sprintf(`<!doctype html><body style="margin:0;background:#000;display:flex;gap:8px"><img src="file://%s" width="%d" height="%d"><img src="file://%s" width="%d" height="%d"></body>`,
		boardPNG, sc.wpx, sc.hpx, dashPNG, sc.wpx, sc.hpx)
	if err := os.WriteFile(pair, []byte(body), 0o644); err != nil {
		return "", err
	}
	out := filepath.Join(outDir, sc.name+".png")
	if err := chrome("file://"+pair, out, sc.wpx*2+8, sc.hpx); err != nil {
		return "", err
	}
	return out, nil
}
