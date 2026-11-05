// Package link reads doze:// links: what a README, a console, a
// notification or a terminal can hand the app to open it somewhere.
//
//	doze://shop             the shop stack, by its name
//	doze://shop/db          the db service in it
//	doze://open?dir=/abs/path&show=db
//	doze://library          the Library
//	doze://settings         Settings
package link

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

// Scheme is the URL scheme the app answers.
const Scheme = "doze"

// Link is where a link points.
type Link struct {
	// Page is "library", "settings", or "" for a stack.
	Page string
	// Stack is a stack's name; Dir its folder, when the link gave one.
	Stack, Dir string
	// Show is a service in that stack.
	Show string
}

// Is says s is a doze:// link.
func Is(s string) bool { return strings.HasPrefix(strings.ToLower(s), Scheme+"://") }

// Parse reads a doze:// link.
func Parse(s string) (Link, error) {
	u, err := url.Parse(s)
	if err != nil || !strings.EqualFold(u.Scheme, Scheme) {
		return Link{}, errors.New("not a doze:// link")
	}
	host := strings.ToLower(u.Host)
	path := strings.Trim(u.Path, "/")
	switch host {
	case "library", "settings":
		return Link{Page: host}, nil
	case "open":
		dir := u.Query().Get("dir")
		if dir == "" || !filepath.IsAbs(dir) {
			return Link{}, errors.New("doze://open needs dir=, an absolute folder")
		}
		return Link{Dir: filepath.Clean(dir), Show: u.Query().Get("show")}, nil
	case "":
		return Link{}, errors.New("a doze:// link names a stack: doze://shop/db")
	}
	l := Link{Stack: u.Host}
	if path != "" {
		l.Show = strings.SplitN(path, "/", 2)[0]
	}
	return l, nil
}
