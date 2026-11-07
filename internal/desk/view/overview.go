package view

import (
	"strings"

	"github.com/doze-dev/doze/internal/control"
	"github.com/doze-dev/doze/internal/display"
)

// addressOf is a row's address as the overview shows it, and the URL Open
// uses: an app's https domain when it has one, else the port-less http
// name the front door serves, else a module's name and port. A task has
// none.
func addressOf(v control.InstanceView) (address, url string) {
	if v.Kind == "task" {
		return "", ""
	}
	if v.HTTPS != "" {
		return v.HTTPS, v.HTTPS
	}
	if strings.HasPrefix(v.Resource, "http://") || strings.HasPrefix(v.Resource, "https://") {
		return v.Resource, v.Resource
	}
	addr, port := display.URL(v)
	switch {
	case addr == "":
		return "", ""
	case strings.HasPrefix(addr, "http") && v.Kind == "module" && v.URL == "" && v.Resource == "":
		// A module is web only when it says so; its name alone is not.
		return display.Address(v), ""
	case strings.HasPrefix(addr, "http"):
		// Whole, http:// and all: a browser takes a bare .doze name for a
		// search.
		return addr + port, addr + port
	}
	return addr + port, ""
}
