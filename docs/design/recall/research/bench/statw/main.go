package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func main() {
	root := os.Args[1]
	list := os.Args[2]
	f, _ := os.Open(list)
	var paths []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		paths = append(paths, filepath.Join(root, sc.Text()))
	}
	for r := 0; r < 3; r++ {
		t := time.Now()
		for _, p := range paths {
			os.Lstat(p)
		}
		ser := time.Since(t)
		t = time.Now()
		var wg sync.WaitGroup
		ch := make(chan string, 1024)
		for w := 0; w < 8; w++ {
			wg.Add(1)
			go func() { defer wg.Done(); for p := range ch { os.Lstat(p) } }()
		}
		for _, p := range paths {
			ch <- p
		}
		close(ch)
		wg.Wait()
		par := time.Since(t)
		t = time.Now()
		n := 0
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if d != nil && d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			n++
			return nil
		})
		walk := time.Since(t)
		fmt.Printf("files=%d lstat serial=%v (%.1fus/file) lstat 8-way=%v walkdir(no stat)=%v entries=%d\n", len(paths), ser.Round(time.Millisecond), float64(ser.Microseconds())/float64(len(paths)), par.Round(time.Millisecond), walk.Round(time.Millisecond), n)
	}
}
