// The web UI's shell. The single-page app (web/, embedded through
// internal/webui) owns every page route: Go decides the status code and
// the head tags a crawler reads, React draws the body. The shell carries
// no data — everything it shows comes from /api/ui/ (api.go), which is
// where access is decided — so a page route may serve it without looking
// anything up. The hashed bundles ride under /static/app/, inheriting the
// static surface's public, sessionless access.

package server

import (
	"bytes"
	"cmp"
	"html/template"
	"net/http"
	"strings"

	"github.com/gocov/gocov/internal/webui"
)

const (
	spaAssetPrefix = "/static/app/"
	// appPrefix is where the app lived while it was built beside the
	// template pages. Kept for one release as a redirect to the canonical
	// URL it now answers directly.
	appPrefix = "/app"
)

// shellTitle is the one tag web/index.html is contracted to carry
// verbatim, and the anchor head injection replaces.
const shellTitle = "<title>gocov</title>"

// appHead is what a page route adds to the shell: its own title, and the
// tags that used to sit in a template's "head" block — a description and
// canonical link, or a robots directive. Extra is raw HTML, so whoever
// builds it escapes every dynamic value (see seo.go).
type appHead struct {
	Title string
	Extra string
}

func (s *Server) spaRoutes() {
	assets := http.StripPrefix(spaAssetPrefix, http.FileServerFS(webui.FS()))
	s.mux.Handle("GET "+spaAssetPrefix, spaAssetCache(assets))
	s.mux.HandleFunc("GET "+appPrefix, redirectFromAppPrefix)
	s.mux.HandleFunc("GET "+appPrefix+"/{path...}", redirectFromAppPrefix)
}

// redirectFromAppPrefix sends a pre-cutover /app/… link to the same path
// without the prefix, which is now the page's own URL.
func redirectFromAppPrefix(w http.ResponseWriter, r *http.Request) {
	// EscapedPath, not Path: a GitLab workspace prefix rides as one %2F
	// segment and must still be one when the browser comes back.
	target := cmp.Or(strings.TrimPrefix(r.URL.EscapedPath(), appPrefix), "/")
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

// serveApp writes the app shell with the given status and head. The
// caller decided the status — 200 for a page the viewer may see, 404 for
// one that is missing or hidden — and the app draws the matching screen.
// A Cache-Control the caller already set is left alone: the anonymous
// public-report path marks its render briefly cacheable (scope.go).
func (s *Server) serveApp(w http.ResponseWriter, r *http.Request, status int, head appHead) {
	// Content-Type must precede WriteHeader; after the status is written
	// the header map is frozen.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if w.Header().Get("Cache-Control") == "" {
		// The shell names the current bundle hashes, so it must never be stale.
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := w.Write(injectHead(appShell(), head)); err != nil {
		s.log.Warn("writing app shell", "path", r.URL.Path, "err", err)
	}
}

// appShell is the built shell, or the built-in stand-in when the web
// build has not run. Both carry shellTitle and an empty #root, so every
// status code, redirect and head injection is testable with the Go
// toolchain alone.
func appShell() []byte {
	if index, ok := webui.Index(); ok {
		return index
	}
	return []byte(fallbackShell)
}

const fallbackShell = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>gocov</title>
</head>
<body>
<div id="root"></div>
<p>The web UI was not built into this binary: run <code>npm ci &amp;&amp; npm run build</code> in web/ and rebuild.</p>
</body>
</html>
`

// injectHead replaces the shell's one <title> tag with the page's title
// and whatever head tags follow it. The title is escaped here; Extra is
// built escaped by its caller.
func injectHead(shell []byte, head appHead) []byte {
	if head.Title == "" && head.Extra == "" {
		return shell
	}
	replacement := "<title>" + template.HTMLEscapeString(cmp.Or(head.Title, "gocov")) + "</title>" + head.Extra
	return bytes.Replace(shell, []byte(shellTitle), []byte(replacement), 1)
}

// handleAppPage serves the shell for a page route whose access question
// requireAuth has already answered: the dashboard, onboarding, the
// settings pages and the component gallery. The shell carries no data,
// so membership is /api/ui's to enforce, not this route's.
func (s *Server) handleAppPage(w http.ResponseWriter, r *http.Request) {
	s.serveApp(w, r, http.StatusOK, appHead{})
}

// spaAssetCache marks Vite's content-hashed bundles immutable; anything
// else in the build (index.html, the favicon) revalidates.
func spaAssetCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, spaAssetPrefix+"assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}
