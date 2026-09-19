import { queryOptions } from "@tanstack/react-query";
import { apiGet } from "./client";
import type {
  Dashboard, LoginInfo, OnboardingInfo, RepoPage, RepoSettings, Session, SetupInfo, SetupStatus, SourcePage, UploadPage, WorkspaceSettings,
} from "./types";

// One factory per endpoint: pages pass these to useQuery, tests and
// mutations reuse the keys.

const qs = (params: Record<string, string | number | undefined>) => {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== "" && v !== 0) p.set(k, String(v));
  const s = p.toString();
  return s ? "?" + s : "";
};

/** Slugs and source paths carry slashes that must stay slashes. */
const segs = (path: string) => path.split("/").map(encodeURIComponent).join("/");

export const sessionQuery = () =>
  queryOptions({ queryKey: ["session"], queryFn: () => apiGet<Session>("/session"), staleTime: 5 * 60_000 });

export const loginQuery = (denied: boolean) =>
  queryOptions({ queryKey: ["login", denied], queryFn: () => apiGet<LoginInfo>("/login" + (denied ? "?denied=1" : "")) });

export const dashboardQuery = (ws: string) =>
  queryOptions({ queryKey: ["dashboard", ws], queryFn: () => apiGet<Dashboard>("/dashboard" + qs({ ws })) });

export const repoQuery = (forge: string, slug: string, branch: string, page: number) =>
  queryOptions({
    queryKey: ["repo", forge, slug, branch, page],
    queryFn: () => apiGet<RepoPage>(`/repos/${encodeURIComponent(forge)}/${segs(slug)}` + qs({ branch, page })),
  });

export const uploadQuery = (id: string) =>
  queryOptions({ queryKey: ["upload", id], queryFn: () => apiGet<UploadPage>(`/uploads/${encodeURIComponent(id)}`) });

export const sourceQuery = (id: string, path: string) =>
  queryOptions({
    queryKey: ["source", id, path],
    queryFn: () => apiGet<SourcePage>(`/uploads/${encodeURIComponent(id)}/files/${segs(path)}`),
  });

export const workspaceSettingsPath = (forge: string, prefix: string) =>
  `/workspaces/${encodeURIComponent(forge)}/${encodeURIComponent(prefix)}`;

export const workspaceSettingsQuery = (forge: string, prefix: string) =>
  queryOptions({
    queryKey: ["workspace-settings", forge, prefix],
    queryFn: () => apiGet<WorkspaceSettings>(workspaceSettingsPath(forge, prefix)),
  });

/** action "" = the settings document itself; the verb rides before the slug. */
export const repoSettingsPath = (forge: string, slug: string, action = "") =>
  `/repo-settings/${action ? action + "/" : ""}${encodeURIComponent(forge)}/${segs(slug)}`;

export const repoSettingsQuery = (forge: string, slug: string) =>
  queryOptions({
    queryKey: ["repo-settings", forge, slug],
    queryFn: () => apiGet<RepoSettings>(repoSettingsPath(forge, slug)),
  });

export const onboardingQuery = () => queryOptions({ queryKey: ["onboarding"], queryFn: () => apiGet<OnboardingInfo>("/onboarding") });

export const setupQuery = (forge: string, prefix: string) =>
  queryOptions({
    queryKey: ["setup", forge, prefix],
    queryFn: () => apiGet<SetupInfo>(workspaceSettingsPath(forge, prefix) + "/setup"),
  });

/** Poll with `refetchInterval` while waiting for the first upload. */
export const setupStatusQuery = (forge: string, prefix: string) =>
  queryOptions({
    queryKey: ["setup-status", forge, prefix],
    queryFn: () => apiGet<SetupStatus>(workspaceSettingsPath(forge, prefix) + "/setup/status"),
  });

