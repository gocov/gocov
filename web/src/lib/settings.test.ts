import type { Gate, RepoSettings, WorkspaceSettings } from "./api/types";
import {
  gateActive,
  gateLabel,
  ignorePatterns,
  patternLabel,
  repoInput,
  workspaceInput,
} from "./settings";

const gate = (g: Partial<Gate> = {}): Gate => ({
  min_coverage: null,
  min_diff_coverage: null,
  max_coverage_drop: null,
  ...g,
});

test("gateActive counts only the rules that are set", () => {
  expect(gateActive(gate())).toBe(0);
  expect(gateActive(gate({ min_coverage: 0 }))).toBe(1);
  expect(gateActive(gate({ min_coverage: 80, max_coverage_drop: 2 }))).toBe(2);
});

test("gateLabel says how many, or that there are none", () => {
  expect(gateLabel(gate())).toBe("None active");
  expect(gateLabel(gate({ min_coverage: 80 }))).toBe("1 active");
  expect(gateLabel(gate({ min_coverage: 80, min_diff_coverage: 70 }))).toBe("2 active");
});

test("ignorePatterns drops blanks and comments, splits on lines and commas", () => {
  expect(ignorePatterns("vendor/**\n\n  # generated\n**/*.pb.go, *_mock.go\n")).toEqual([
    "vendor/**",
    "**/*.pb.go",
    "*_mock.go",
  ]);
  expect(patternLabel("")).toBe("None");
  expect(patternLabel("vendor/**")).toBe("1 pattern");
  expect(patternLabel("vendor/**\n*_mock.go")).toBe("2 patterns");
});

const workspace: WorkspaceSettings = {
  workspace: {
    forge: "github",
    prefix: "acme",
    default_branch: "main",
    report_retention_days: 90,
    gate: gate({ min_coverage: 80 }),
  },
  owner: true,
  repo_count: 3,
  reporting: { available: true, state: "on", account: "", connect_url: "https://github.com/apps/gocov" },
  server_url: null,
  token_masked: "gocov_live_••••",
};

test("workspaceInput takes only the editable half, detached from the document", () => {
  const input = workspaceInput(workspace);
  expect(input).toEqual({ default_branch: "main", report_retention_days: 90, gate: gate({ min_coverage: 80 }) });
  input.gate.min_coverage = 50;
  expect(workspace.workspace.gate.min_coverage).toBe(80);
});

const repo: RepoSettings = {
  repo: {
    forge: "github",
    slug: "acme/api",
    default_branch: "main",
    gate: gate({ min_diff_coverage: 70 }),
    ignore_paths: "vendor/**",
    public_reports: true,
    badge_url: "/badge/github/acme/api",
    badge_markdown: "![coverage](/badge/github/acme/api)",
  },
  workspace: { forge: "github", prefix: "acme" },
  owner: true,
  show_public_reports: true,
  token_masked: "gocov_live_••••",
};

test("repoInput covers every saved field", () => {
  const saved = repoInput(repo);
  expect(saved).toEqual({
    default_branch: "main",
    gate: gate({ min_diff_coverage: 70 }),
    ignore_paths: "vendor/**",
    public_reports: true,
  });
});
