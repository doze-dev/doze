// Package frontend carries the app's page: React, Vite and TypeScript under
// src, built by bun into dist (make frontend) and embedded here so the
// binary is one file. dist is committed with a placeholder only, so a build
// without bun still compiles; the web package then serves a note instead.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist is the built page's files, rooted at dist.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// Built says whether the page is in this binary.
func Built() bool {
	_, err := fs.Stat(Dist(), "index.html")
	return err == nil
}
