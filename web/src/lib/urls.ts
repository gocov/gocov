// In-app routes and the few server endpoints the SPA still links out to.
// Mirrors internal/server/urls.go: tenant URLs carry the forge first, and
// both slugs and workspace prefixes keep their slashes (GitLab groups nest:
// grp/sub) — which is why a workspace's page is named before the workspace,
// not after it: the prefix is the route's trailing splat.

export const routes = {
  /** `ws` is "forge/prefix"; without one the server picks the viewer's first workspace. */
  dashboard: (ws?: string) => (ws ? `/w/${ws}` : "/"),
  repo: (forge: string, slug: string) => `/repos/${forge}/${slug}`,
  upload: (id: number | string) => `/uploads/${id}`,
  /** `merged`: the file as every part of the upload's commit reports it, not this upload alone. */
  source: (id: number | string, path: string, merged = false) =>
    `/uploads/${id}/files/${path}${merged ? "?parts=merged" : ""}`,
  workspace: (forge: string, prefix: string) => `/workspace-settings/${forge}/${prefix}`,
  repoSettings: (forge: string, slug: string) => `/repo-settings/${forge}/${slug}`,
  /** Choose or create a workspace. */
  onboarding: () => "/onboarding",
  /** "Add a repository": the CI snippet for a workspace. */
  workspaceSetup: (forge: string, prefix: string) => `/workspace-setup/${forge}/${prefix}`,
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
