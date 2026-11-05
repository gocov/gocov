// The UI request log: one line per /api/ui/ request with its route, status
// and duration, so a slow screen shows up by route rather than by the
// tenant-specific URL it was asked for.

package server

import (
	"net/http"
	"time"
)

// logUIRequest wraps a UI API handler to log how long it took. It logs the
// route pattern (r.Pattern, set by the mux), never the path: the path names
// the repository or workspace, and a private one's name does not belong in
// the logs any more than in a 404.
func (s *Server) logUIRequest(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		h(sw, r)
		s.log.Info("ui request",
			"route", r.Pattern,
			"status", sw.status(),
			"ms", time.Since(start).Milliseconds(),
		)
	}
}

// statusWriter records the status code a handler wrote. Unwrap keeps
// http.ResponseController reaching the underlying writer.
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// status is the code the handler wrote, 200 when it wrote nothing (the
// server sends 200 for an empty response).
func (w *statusWriter) status() int {
	if w.code == 0 {
		return http.StatusOK
	}
	return w.code
}
