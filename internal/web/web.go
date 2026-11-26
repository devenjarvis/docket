// Package web holds the UI: one HTML page, its stylesheet (the whole design
// system), its script and its fonts. Nothing is fetched from the network.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var files embed.FS

// FS is the static directory.
func FS() fs.FS {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
