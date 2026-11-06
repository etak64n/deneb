// Package web holds the dashboard and the embed page. `pnpm build` writes them to
// dist, and the deneb binary carries them inside.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built files, with index.html and embed.html at the root.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
