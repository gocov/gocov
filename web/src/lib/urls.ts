// In-app routes and the few server endpoints the SPA still links out to.
// Mirrors internal/server/urls.go: tenant URLs carry the forge first; slugs
// keep their slashes, workspace prefixes are one escaped segment (GitLab
// groups nest).

export const routes = {
  dashboard: (ws?: string) => (ws ? `/?ws=${encodeURIComponent(ws)}` : "/"),
  repo: (forge: string, slug: string) => `/repos/${forge}/${slug}`,
  upload: (id: number | string) => `/uploads/${id}`,
  source: (id: number | string, path: string) => `/uploads/${id}/files/${path}`,
  workspace: (forge: string, prefix: string) => `/workspaces/${forge}/${encodeURIComponent(prefix)}`,
  repoSettings: (forge: string, slug: string) => `/repo-settings/${forge}/${slug}`,
  /** Choose or create a workspace. */
  onboarding: () => "/onboarding",
  /** "Add a repository": the CI snippet for a workspace. */
  workspaceSetup: (forge: string, prefix: string) => `/workspaces/${forge}/${encodeURIComponent(prefix)}/setup`,
  components: () => "/_components",
  /** Sign in, coming back to `next` afterwards. */
  login: (next?: string) => (next ? `/login?next=${encodeURIComponent(next)}` : "/login"),
};

/** Not routes: endpoints the browser leaves the app for. Plain <a>, never <Link>. */
export const server = {
  oauthStart: (forge: string, next: string) => `/oauth/${forge}/start?next=${encodeURIComponent(next)}`,
  logout: () => "/logout",
  docs: (page = "") => `https://docs.gocov.dev/${page}`,
};
