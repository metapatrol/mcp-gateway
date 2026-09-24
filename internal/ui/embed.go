package ui

import (
	"embed"
	"io/fs"
)

// The Angular application in web/ui builds into dist/ui (make ui-build).
// Whatever that directory holds when the binary is built is what the binary
// serves: a `go build` without a UI build embeds only dist/.gitkeep, and the
// console then says that no UI is built. The image and the release build the
// UI first.

//go:embed all:dist
var dist embed.FS

// Built returns the embedded UI, or nil when the binary was built without one.
func Built() fs.FS {
	sub, err := fs.Sub(dist, "dist/ui")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
