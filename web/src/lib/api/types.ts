// The contract between internal/server/api*.go and the SPA. JSON is
// snake_case, like /api/v1. Every timestamp is RFC 3339; every coverage
// figure is a percentage 0–100 (never a fraction); "no value" is null, not 0.
// Formatting (arrows, "3 hours ago", colours) happens client-side.
//
// Upload tokens never appear in a GET: settings responses carry a masked
// form only, and the value comes from an explicit reveal-token POST.

export type Forge = "github" | "gitlab" | "bitbucket" | (string & {});

/** Error body of every non-2xx answer. 401 = no session, 404 = missing or hidden. */
export interface ApiErrorBody {
  error: string;
}

// ---- GET /api/ui/session (readable signed-out) ----------------------------

export interface Session {
  user: { display_name: string; email: string } | null;
  /** false = open instance: no sign-in, the shell shows a notice. */
  auth_enabled: boolean;
  hosted: boolean;
  analytics?: { key: string; host: string; user_id?: string };
}

// ---- GET /api/ui/login[?denied=1] (readable signed-out) --------------------

export interface LoginInfo {
  hosted: boolean;
  /** Sign-in starts at /oauth/{name}/start?next=… (server route). */
  providers: { name: Forge }[];
  /** Only disclosed with ?denied=1 on a private instance (the "no access" notice); [] otherwise. */
  tracked_workspaces: { name: string; forge: Forge }[];
}

// ---- GET /api/ui/dashboard?ws={forge}/{prefix} -----------------------------

export interface Dashboard {
  /** Hosted user with no workspace: send them to /onboarding. */
  needs_onboarding: boolean;
  /** Signed in, so may register a workspace. */
  can_onboard: boolean;
  /** null when the viewer has no repos and no workspaces at all. */
  current: WorkspaceGroup | null;
  switcher: WorkspaceGroup[];
  repos: DashRepo[];
  stats: DashStats;
  attention: AttentionItem[];
}

export interface WorkspaceGroup {
  forge: Forge;
  prefix: string;
  repo_count: number;
  /** Statement-weighted, null before any upload. */
  coverage: number | null;
  current: boolean;
  /** A registered workspace (has a settings page). */
  tracked: boolean;
}

export type GateState = "pass" | "fail" | "none";

export interface DashRepo {
  forge: Forge;
  slug: string;
  /** slug without the workspace prefix. */
  name: string;
  coverage: number | null;
  /** vs the last gate-passing report; null when there is no baseline. */
  delta: number | null;
  gate: GateState;
  stale: boolean;
  /** Recent default-branch coverage, oldest first, at most 12 points. */
  series: number[];
  uploaded_at: string | null;
}

export interface DashStats {
  coverage: number | null;
  reporting: "connected" | "not_connected" | "broken";
  /** "gocov[bot]" or the granting account; "" when not connected. */
  reporting_as: string;
}

export interface AttentionItem {
  /** Events only. A repository without a gate is not one: see the table's Gate column. */
  kind: "failing" | "stale";
  forge: Forge;
  slug: string;
  name: string;
  coverage: number | null;
  min_coverage: number | null;
  stale_days: number | null;
}

// ---- shared report pieces -------------------------------------------------

export interface RepoRef {
  forge: Forge;
  slug: string;
}

export interface Gate {
  min_coverage: number | null;
  min_diff_coverage: number | null;
  max_coverage_drop: number | null;
}

export interface BaseRef {
  upload_id: number;
  sha: string;
  coverage: number;
}

export interface Verdict {
  /** neutral = no gate configured. */
  state: "pass" | "fail" | "neutral";
  coverage: number;
  delta: number | null;
  /** Prose walk-through of the gate rules and their outcome. */
  reason: string;
  base: BaseRef | null;
}

export interface FileRow {
  path: string;
  coverage: number;
  covered_stmts: number;
  total_stmts: number;
  /** Uncovered line ranges, "12-18, 40"; the server caps the list and ends it "+N more". */
  uncovered: string;
  /** Coverage of the same path at the baseline; null when absent or no baseline. */
  before: number | null;
  /**
   * The baseline's own statement counts for the path, null with `before`. A
   * directory's "before" is rolled up from these.
   */
  before_covered_stmts: number | null;
  before_total_stmts: number | null;
  new_file: boolean;
  /** Ranges covered at the baseline but not now. */
  newly_uncovered: string;
  source_changed: boolean;
  coverage_changed: boolean;
}

export interface FilesView {
  upload_id: number;
  has_base: boolean;
  files: FileRow[];
}

// ---- GET /api/ui/repos/{forge}/{slug...}?branch= ----------------------------

export interface RepoPage {
  repo: RepoRef & {
    default_branch: string;
    gate: Gate;
    /** Viewer may open the settings page. */
    can_settings: boolean;
  };
  branches: string[];
  /** Branch the summary, trend and files describe. */
  trend_branch: string;
  summary: RepoSummary | null;
  trend: TrendPoint[];
  files: FilesView | null;
}

export interface RepoSummary {
  verdict: Verdict;
  commit: { upload_id: number; sha: string; at: string; branch: string; pr_id: string; is_default: boolean };
  covered_stmts: number;
  total_stmts: number;
  /** ci_provider is a code ("github", "gitlab", "bitbucket"); "" when unknown. */
  last_upload: { at: string; ci_provider: string } | null;
}

export interface TrendPoint {
  upload_id: number;
  sha: string;
  coverage: number;
  at: string;
  gate_failed: boolean;
}

// ---- GET /api/ui/repo-uploads/{forge}/{slug...}?branch=&page= ---------------

/** One page of the repo page's upload history, newest first. */
export interface RepoUploads {
  uploads: UploadRow[];
  has_older: boolean;
}

export interface UploadRow {
  id: number;
  sha: string;
  branch: string;
  pr_id: string;
  coverage: number;
  /** Against the gate the upload was judged by; "none" when no rule was set. */
  gate: "pass" | "fail" | "none";
  at: string;
}

// ---- GET /api/ui/uploads/{id} -----------------------------------------------

export interface UploadPage {
  repo: RepoRef;
  upload: {
    id: number;
    sha: string;
    branch: string;
    pr_id: string;
    at: string;
    commit_message: string;
    commit_author: string;
    /** Fork upload verified by workflow run, not a token. */
    tokenless: boolean;
  };
  verdict: Verdict;
  covered_stmts: number;
  total_stmts: number;
  diff: { coverage: number; covered_lines: number; total_lines: number; changed_files: number; unmatched_files: number } | null;
  /** Always present; `files: []` when the upload has no per-file data. */
  files: FilesView;
  provenance: Provenance;
  /** Raw profile download (server route), null when not allowed. */
  download_url: string | null;
}

/** How an upload arrived, as facts: the card words them (lib/format). */
export interface Provenance {
  received_at: string;
  profile_name: string;
  /** 0 when not recorded. */
  profile_bytes: number;
  format: string;
  /** "github", "gitlab", "bitbucket"; "" when unknown. */
  ci_provider: string;
  ci_run_url: string;
  uploader: string;
  /** "cli", "action"; "" when unknown. */
  uploader_kind: string;
  /** The upload's part, "" for the default single profile. */
  part: string;
  /** Parts merged into the commit's report; 0 when unknown. */
  parts: number;
  /** Server processing time; 0 when not recorded. */
  process_ms: number;
  ignored_files: number;
}

// ---- GET /api/ui/uploads/{id}/files/{path...} -------------------------------

export interface SourcePage {
  repo: RepoRef;
  upload: { id: number; sha: string };
  file: { path: string; coverage: number; covered_stmts: number; total_stmts: number };
  /** null without a baseline, and whenever `unavailable` is set. */
  delta: number | null;
  /** Non-empty: the source could not be fetched, and why. `lines` is then empty. */
  unavailable: string;
  uncovered: string;
  lines: SourceLine[];
}

export interface SourceLine {
  no: number;
  text: string;
  /** null = not a statement. */
  hits: number | null;
  new_miss: boolean;
}

// ---- workspace settings -----------------------------------------------------
// GET    /api/ui/workspace-settings/{forge}/{prefix...}
// POST   /api/ui/workspace-settings/save/{forge}/{prefix...}          WorkspaceSettingsInput → WorkspaceSettings
// POST   /api/ui/workspace-settings/rotate-token/{forge}/{prefix...}  → TokenReveal
// POST   /api/ui/workspace-settings/reveal-token/{forge}/{prefix...}  → TokenReveal
// POST   /api/ui/workspace-settings/disconnect/{forge}/{prefix...}    → WorkspaceSettings
// POST   /api/ui/workspace-settings/delete/{forge}/{prefix...}        → 204
// Mutations are owner-only (403 otherwise).

export interface WorkspaceSettings {
  workspace: {
    forge: Forge;
    prefix: string;
    default_branch: string;
    /** 0 = forever. */
    report_retention_days: number;
    gate: Gate;
  };
  owner: boolean;
  repo_count: number;
  reporting: {
    /** false = this deployment has no connect mechanism for the forge. */
    available: boolean;
    state: "on" | "off" | "broken";
    /** Granting account, "" for the GitHub App (posts as gocov[bot]). */
    account: string;
    /** Server/forge URL that starts connect or install; "" when unavailable. */
    connect_url: string;
  };
  /** Self-hosted instances show GOCOV_SERVER; null on the hosted service. */
  server_url: string | null;
  /** null for non-owners. */
  token_masked: string | null;
}

export interface WorkspaceSettingsInput {
  default_branch: string;
  report_retention_days: number;
  gate: Gate;
}

export interface TokenReveal {
  token: string;
}

// ---- repo settings ----------------------------------------------------------
// GET  /api/ui/repo-settings/{forge}/{slug...}
// POST /api/ui/repo-settings/save/{forge}/{slug...}          RepoSettingsInput → RepoSettings
// POST /api/ui/repo-settings/rotate-token/{forge}/{slug...}  → TokenReveal
// POST /api/ui/repo-settings/reveal-token/{forge}/{slug...}  → TokenReveal
// POST /api/ui/repo-settings/delete/{forge}/{slug...}        → 204

export interface RepoSettings {
  repo: RepoRef & {
    default_branch: string;
    gate: Gate;
    /** One pattern per line. */
    ignore_paths: string;
    public_reports: boolean;
    badge_url: string;
    badge_markdown: string;
  };
  workspace: { forge: Forge; prefix: string };
  owner: boolean;
  /** The forge reports the repo public and the instance allows public reports. */
  show_public_reports: boolean;
  token_masked: string | null;
}

export interface RepoSettingsInput {
  default_branch: string;
  gate: Gate;
  ignore_paths: string;
  public_reports: boolean;
}

// ---- onboarding: choosing a workspace ---------------------------------------
// GET  /api/ui/onboarding            → OnboardingInfo
// POST /api/ui/onboarding/register   RegisterInput → RegisterResult
// Signed-in only; where the instance has no registration (the server's
// registerUser gate) both answer 404.

export interface OnboardingInfo {
  forge: Forge;
  /** Display name of the signed-in account. */
  account: string;
  /**
   * "install": GitHub with the App configured — the workspace is created by
   * installing the app at `install_url` (a forge URL; GitHub sends the
   * browser back to the server afterwards).
   * "pick": choose from the memberships read at sign-in (`rows`).
   */
  mode: "install" | "pick";
  install_url: string;
  rows: OnboardingRow[];
  /** How many memberships the forge reported at sign-in. */
  membership_count: number;
}

export interface OnboardingRow {
  prefix: string;
  /**
   * available  — free: "Create the workspace"
   * member     — registered and the viewer is in: "Open dashboard"
   * registered — registered, membership not synced yet: "Join"
   * unowned    — free, but creating it takes a forge owner/admin
   */
  state: "available" | "member" | "registered" | "unowned";
}

export interface RegisterInput {
  prefix: string;
}

export interface RegisterResult {
  forge: Forge;
  prefix: string;
  /** false = joined an existing workspace. */
  created: boolean;
}

// ---- onboarding: wiring CI and the first report -----------------------------
// GET /api/ui/workspace-setup/{forge}/{prefix...}         → SetupInfo   (members)
// GET /api/ui/workspace-setup-status/{forge}/{prefix...}  → SetupStatus (members; polled every 3s)
// The upload token comes from the workspace reveal-token POST (owners).

export interface SetupInfo {
  workspace: { forge: Forge; prefix: string };
  owner: boolean;
  /**
   * Uploads can authenticate with a forge-minted OIDC identity token (the
   * workspace is connected and the connection works): the snippet needs no secret.
   */
  tokenless: boolean;
  /** A connection exists but no longer works; reconnecting restores tokenless. */
  connection_broken: boolean;
  /** This server's public URL: the OIDC audience, and GOCOV_SERVER. */
  base_url: string;
  /** true on the hosted service: the CLI defaults to it, so snippets omit the server. */
  server_implicit: boolean;
  /** GitLab only: the CI/CD Catalog component is usable (the instance's GitLab is gitlab.com). */
  gitlab_catalog: boolean;
  /** The CLI release the raw-download snippets pin, in the form "v1.2.3". */
  cli_version: string;
  /** null for non-owners. */
  token_masked: string | null;
  reporting: WorkspaceSettings["reporting"];
  status: SetupStatus;
}

export interface SetupStatus {
  /** Repositories registered under the workspace so far. */
  repo_count: number;
  /** The newest report among them; null until one has coverage. */
  first_report: {
    repo: RepoRef;
    branch: string;
    sha: string;
    coverage: number;
    covered_stmts: number;
    total_stmts: number;
  } | null;
  /** "Commit status posted as gocov[bot]." — "" when nothing was posted back. */
  reports_posted: string;
}

