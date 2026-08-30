// Who is signed in, and the middleware that insists on it: the session
// cookie, its server-side row, and the redirect a signed-out visitor gets
// on a page that is not public.

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gocov/gocov/internal/store"
)

// Session lifetime is fixed (no sliding renewal in M1); logout revokes
// server-side immediately, and dropped forge membership is re-checked at
// each sign-in.
const sessionTTL = 30 * 24 * time.Hour

const (
	sessionCookie = "gocov_session"
	// stateCookie binds the OAuth callback to the browser that started the
	// flow (CSRF/replay protection). It also carries the in-site path to
	// return to after login.
	stateCookie = "gocov_oauth_state"
)

type ctxKey int

const userKey ctxKey = 0

// currentUser returns the signed-in user, or nil on public pages and when
// auth is not configured.
func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

func withUser(ctx context.Context, u *store.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// authEnabled reports whether at least one sign-in provider is
// configured — the switch between the open UI and enforced sign-in.
func (s *Server) authEnabled() bool { return len(s.auths) > 0 }

// requireAuth is the enforcement middleware. With no provider configured
// the UI stays open exactly as before (the layout shows a banner instead);
// with one configured, every non-public path needs a valid session — except
// the report pages of repos that may be public, which pass through
// sessionless for the handler to decide by the repo's effective state.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authEnabled() || publicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		u := s.sessionUser(r)
		if u == nil {
			if s.publicReportCandidate(r) {
				// No session and possibly no login wall: the handler
				// checks whether the repo behind the path is effectively
				// public and redirects to login itself when it is not,
				// so a signed-out browser learns nothing it could not
				// see before these pages existed. No no-store header:
				// the render is anonymous and cacheable.
				next.ServeHTTP(w, r)
				return
			}
			redirectToLogin(w, r)
			return
		}
		// Protected pages must not be served from cache after logout.
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}

// publicReportCandidate reports whether a sessionless request may reach
// its handler for a per-repo public decision: a GET (or HEAD — the mux's
// GET patterns serve those too, and crawlers probe with them) on the
// read-only report pages, and only while the instance-level switch
// (GOCOV_PUBLIC_REPORTS) is on. Everything mutating or administrative
// keeps the login wall unconditionally.
func (s *Server) publicReportCandidate(r *http.Request) bool {
	if !s.publicReports || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return false
	}
	return strings.HasPrefix(r.URL.Path, "/repos/") ||
		strings.HasPrefix(r.URL.Path, "/uploads/")
}

// publicPath reports whether a path must work without a session: the CI
// surface (upload API, badges, health), embedded assets, the login
// flow itself and the crawler files. Everything else is a protected page.
func publicPath(p string) bool {
	if p == "/api/v1/upload" || p == "/healthz" || p == "/login" ||
		p == "/github/webhook" || p == "/robots.txt" || p == "/sitemap.xml" {
		return true
	}
	return strings.HasPrefix(p, "/badge/") ||
		strings.HasPrefix(p, "/static/") ||
		strings.HasPrefix(p, "/oauth/")
}

// sessionUser resolves the session cookie to a user, or nil.
func (s *Server) sessionUser(r *http.Request) *store.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := s.store.UserBySession(r.Context(), hashToken(c.Value))
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log.Error("session lookup", "err", err)
		}
		return nil
	}
	return u
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	next := r.URL.Path
	if r.URL.RawQuery != "" {
		next += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusFound)
}

// handleLogout implements POST /logout: the session dies server-side, so a
// saved cookie or the back button cannot restore access.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if err := s.store.DeleteSession(r.Context(), hashToken(c.Value)); err != nil && !errors.Is(err, store.ErrNotFound) {
			s.internalError(w, "deleting session", err)
			return
		}
	}
	clearCookie(w, sessionCookie, s.secureCookies)
	if !s.authEnabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

// hashToken is what the sessions table stores instead of the token: a DB
// leak then reveals nothing that authenticates.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func clearCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
