// Package dotdoze makes the .doze folder beside a project's doze.hcl.
package dotdoze

import (
	"errors"
	"os"
	"path/filepath"
)

// ignore is the .gitignore doze writes into a project's .doze folder. What
// doze keeps there (endpoints.yaml, state.json) is this machine's state, so
// none of it is committed, and the person's own .gitignore needs no edit.
const ignore = "# Written by doze: this folder is this machine's state, so nothing in it is committed.\n*\n"

// Ensure creates dir if it is missing. When dir is a .doze folder it also
// writes the .gitignore above, once: an existing one is left alone, so a
// person can change it.
func Ensure(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if filepath.Base(dir) != ".doze" {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(dir, ".gitignore"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = f.WriteString(ignore)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
