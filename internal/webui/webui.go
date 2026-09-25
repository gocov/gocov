// Package webui embeds the built single-page web UI. The source lives in
// web/ and Vite builds it into dist/ here; release builds and the Docker
// image run that build before compiling. A checkout that has not run it
// embeds only the placeholder, so the Go toolchain alone still builds and
// tests everything — the server just has no SPA to serve.
package webui

import (
	"embed"
	"io/fs"
	"sync"
)

//go:embed all:dist
var dist embed.FS

// FS is the built site: index.html at the root, hashed bundles under
// assets/.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // "dist" is a constant, valid path
	}
	return sub
}

// Index returns the SPA shell, or false when the web build has not run.
// The embedded build never changes, so it is read once; callers share the
// bytes and must not modify them.
func Index() ([]byte, bool) {
	b, err := index()
	return b, err == nil
}

var index = sync.OnceValues(func() ([]byte, error) {
	return fs.ReadFile(FS(), "index.html")
})
