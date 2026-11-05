//go:build !darwin

package adminprompt

import "errors"

const supported = false

func run(string, string) error { return errors.New("no administrator prompt on this system") }
