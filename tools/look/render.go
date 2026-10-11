package main

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// chromePath is the browser that draws the boards. The dash is drawn without
// one (raster.go); the boards are HTML, so they need it, once: the images are
// kept and only drawn again when a board changes.
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

// boardImage is the board drawn at its size, from the cache when it is current.
func boardImage(root, outDir, name string, wpx, hpx int) (image.Image, error) {
	src := filepath.Join(root, "docs/design/next/boards", name+".dc.html")
	css := filepath.Join(root, "docs/design/next/boards/term.css")
	cache := filepath.Join(outDir, "boards", name+".png")
	if st, err := os.Stat(cache); err == nil {
		fresh := true
		for _, f := range []string{src, css} {
			if fs, err := os.Stat(f); err == nil && fs.ModTime().After(st.ModTime()) {
				fresh = false
			}
		}
		if fresh {
			return readPNG(cache)
		}
	}
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		return nil, err
	}
	cmd := exec.Command(chromePath(), "--headless=new", "--disable-gpu", "--hide-scrollbars",
		"--force-device-scale-factor=1", "--virtual-time-budget=4000",
		fmt.Sprintf("--window-size=%d,%d", wpx, hpx), "--screenshot="+cache, "file://"+src)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("drawing board %s needs Chrome (set CHROME): %w\n%s", name, err, out)
	}
	return readPNG(cache)
}

func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// drawScreen writes what the session's terminal shows as an image.
func drawScreen(s *session, out string) error {
	return writePNG(out, renderScreen(s))
}

// pair puts the board and the dash side by side on one canvas.
func pair(board, dash image.Image) image.Image {
	const gap = 8
	h := max(board.Bounds().Dy(), dash.Bounds().Dy())
	w := board.Bounds().Dx() + gap + dash.Bounds().Dx()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), image.Black, image.Point{}, draw.Src)
	draw.Draw(out, board.Bounds(), board, board.Bounds().Min, draw.Src)
	at := image.Pt(board.Bounds().Dx()+gap, 0)
	draw.Draw(out, dash.Bounds().Add(at), dash, dash.Bounds().Min, draw.Src)
	return out
}

// run plays a scene and writes the dash's image and the pair, returning the
// pair's path.
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

	dash := renderScreen(sess)
	if err := writePNG(filepath.Join(outDir, sc.name+".dash.png"), dash); err != nil {
		return "", err
	}
	board, err := boardImage(root, outDir, sc.board, sc.wpx, sc.hpx)
	if err != nil {
		return "", err
	}
	out := filepath.Join(outDir, sc.name+".png")
	return out, writePNG(out, pair(board, dash))
}
