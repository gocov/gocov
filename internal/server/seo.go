// The crawler surface of public report pages: the head tags each page
// route injects into the app shell (spa.go), robots.txt, which keeps the
// login flow, the settings pages and raw profile downloads out of search
// indexes, and sitemap.xml, which lists every effectively public repo
// page. The latter two are sessionless (see publicPath).

package server

import (
	"encoding/xml"
	"html/template"
	"net/http"
	"strings"

	"github.com/gocov/gocov/internal/core"
	"github.com/gocov/gocov/internal/store"
)

// noindexHead keeps a page out of search indexes while its links still
// count: the upload and source views, which are per-commit detail under
// an indexable repo page.
const noindexHead = `<meta name="robots" content="noindex, follow">`

// repoPageHead is the repo page's crawler surface: the title a result
// lists, the description under it and the canonical URL, so the branch
// and page query parameters do not split one page into many. Handlers
// build it only after the access decision passed — the slug is the one
// thing a refused visitor must not read back (D3).
func (s *Server) repoPageHead(repo *store.Repo) appHead {
	slug := template.HTMLEscapeString(repo.Slug)
	return appHead{
		Title: repo.Slug + " code coverage — gocov",
		Extra: `<meta name="description" content="Code coverage for ` + slug +
			`, tracked by gocov: current total, coverage trend and where coverage is missing.">` +
			"\n" + `<link rel="canonical" href="` +
			template.HTMLEscapeString(s.baseURL+repoURL(repo)) + `">`,
	}
}

// uploadPageHead titles one upload's report and keeps it out of indexes.
func uploadPageHead(repo *store.Repo, upload *store.Upload) appHead {
	return appHead{Title: repo.Slug + " @ " + core.ShortSHA(upload.CommitSHA) + " — gocov", Extra: noindexHead}
}

// sourcePageHead titles one file's source view and keeps it out of
// indexes. The path comes straight off the URL, so it is escaped like
// every other dynamic value the head carries.
func sourcePageHead(path string) appHead {
	return appHead{Title: path + " — gocov", Extra: noindexHead}
}

// handleRobots implements GET /robots.txt. The disallow list names the
// pages that exist but should never rank: the login and OAuth flows, the
// administrative pages (which 404 or redirect for crawlers anyway) and
// the raw profile downloads under otherwise indexable upload pages.
func (s *Server) handleRobots(w http.ResponseWriter, r *http.Request) {
	var sb strings.Builder
	sb.WriteString("User-agent: *\n")
	sb.WriteString("Disallow: /login\n")
	sb.WriteString("Disallow: /oauth/\n")
	sb.WriteString("Disallow: /repo-settings/\n")
	sb.WriteString("Disallow: /w/\n")
	sb.WriteString("Disallow: /workspace-settings/\n")
	sb.WriteString("Disallow: /workspace-setup/\n")
	sb.WriteString("Disallow: /workspace-connect/\n")
	sb.WriteString("Disallow: /workspaces/\n")
	sb.WriteString("Disallow: /uploads/*/profile\n")
	sb.WriteString("Disallow: /api/\n")
	if s.publicReports {
		sb.WriteString("\nSitemap: " + s.baseURL + "/sitemap.xml\n")
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(sb.String()))
}

// sitemapMaxEntries is the sitemap protocol's cap on URLs per file.
const sitemapMaxEntries = 50000

// sitemap is the minimal urlset shape of the sitemap protocol.
type sitemap struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc string `xml:"loc"`
}

// handleSitemap implements GET /sitemap.xml: one entry per effectively
// public repo page, so every public repo is a page search engines can
// find. With public reports off the sitemap does not exist.
func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	if !s.publicReports {
		http.NotFound(w, r)
		return
	}
	refs, err := s.store.PublicRepoRefs(r.Context(), sitemapMaxEntries)
	if err != nil {
		s.internalError(w, "listing public repos for sitemap", err)
		return
	}
	base := s.baseURL
	sm := sitemap{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, ref := range refs {
		sm.URLs = append(sm.URLs, sitemapURL{Loc: base + repoPath(ref.Forge, ref.Slug)})
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(sm); err != nil {
		s.log.Error("encoding sitemap", "err", err)
	}
}
