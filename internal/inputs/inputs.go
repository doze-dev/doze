// Package inputs matches the file globs a `task` lists as its `inputs`, and
// hashes what they match. A task re-runs when the hash changes; the watcher
// uses the same matcher to decide which changes matter.
//
// A pattern is a path relative to the task's directory. `*` and `?` match
// within one path segment, and a segment of `**` matches any number of
// segments, including none.
package inputs

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Match reports whether the slash-separated relative path name matches
// pattern.
func Match(pattern, name string) bool {
	return matchSegs(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegs(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// A run of ** is one.
			for len(pat) > 0 && pat[0] == "**" {
				pat = pat[1:]
			}
			if len(pat) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchSegs(pat, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], name[0]); err != nil || !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

// root is the directory a pattern can be matched under: its segments before
// the first one that has a wildcard in it.
func root(pattern string) string {
	var keep []string
	for _, seg := range strings.Split(pattern, "/") {
		if strings.ContainsAny(seg, "*?[") {
			break
		}
		keep = append(keep, seg)
	}
	return strings.Join(keep, "/")
}

// skipped are directories a pattern walks into only by naming them.
var skipped = map[string]bool{".git": true, "node_modules": true, ".doze": true}

// Files returns the files under dir that any pattern matches, as sorted
// slash-separated paths relative to dir.
func Files(dir string, patterns []string) ([]string, error) {
	found := map[string]bool{}
	for _, pat := range patterns {
		pat = strings.TrimPrefix(filepath.ToSlash(pat), "./")
		base := root(pat)
		start := filepath.Join(dir, filepath.FromSlash(base))
		info, err := os.Stat(start)
		if err != nil {
			continue // nothing there yet is not an error: nothing matches
		}
		if !info.IsDir() {
			if Match(pat, base) {
				found[base] = true
			}
			continue
		}
		err = filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, rerr := filepath.Rel(dir, p)
			if rerr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if p != start && skipped[d.Name()] && !strings.Contains(pat, d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if Match(pat, rel) {
				found[rel] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	out := make([]string, 0, len(found))
	for f := range found {
		out = append(out, f)
	}
	sort.Strings(out)
	return out, nil
}

// Hash is a digest of the paths and contents of the files the patterns match,
// and how many there are. No match is a valid, stable hash.
func Hash(dir string, patterns []string) (string, int, error) {
	files, err := Files(dir, patterns)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	for _, rel := range files {
		f, err := os.Open(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			continue // gone between the walk and the read
		}
		fh := sha256.New()
		_, cerr := io.Copy(fh, f)
		f.Close()
		if cerr != nil {
			continue
		}
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(fh.Sum(nil))
	}
	return hex.EncodeToString(h.Sum(nil)), len(files), nil
}
