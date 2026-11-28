// Pure helpers the two settings pages share: what a gate says about
// itself, what the ignore box holds, and the editable half of each settings
// document. No React, no fetching — every one of these
// is a function of its arguments.

import type { Gate, RepoSettings, RepoSettingsInput, WorkspaceSettings, WorkspaceSettingsInput } from "./api/types";
import { plural } from "./format";

/** How many of the three rules are switched on; null means off. */
export function gateActive(gate: Gate): number {
  return [gate.min_coverage, gate.min_diff_coverage, gate.max_coverage_drop].filter((v) => v !== null).length;
}

/** The gates card's header chip: "2 active" — or that none are. */
export function gateLabel(gate: Gate): string {
  const n = gateActive(gate);
  return n === 0 ? "None active" : `${n} active`;
}

/**
 * The patterns in the ignore box, the way internal/ignore.Parse counts
 * them: one per line or comma, blanks and `#` comments dropped.
 */
export function ignorePatterns(text: string): string[] {
  return text
    .split(/[\n\r,]/)
    .map((line) => line.trim())
    .filter((line) => line !== "" && !line.startsWith("#"));
}

/** The ignore card's header chip: "3 patterns" — or that there are none. */
export function patternLabel(text: string): string {
  const n = ignorePatterns(text).length;
  return n === 0 ? "None" : plural(n, "pattern");
}

/**
 * The part names in the carryforward box, the way the server stores them
 * (parseCarryforwardParts): split as the ignore box is, lowercased, each
 * once.
 */
export function carriedParts(text: string): string[] {
  return [...new Set(ignorePatterns(text).map((name) => name.toLowerCase()))];
}

/** The carryforward card's header chip: "2 parts" — or that there are none. */
export function carriedLabel(text: string): string {
  const n = carriedParts(text).length;
  return n === 0 ? "None" : plural(n, "part");
}

/** The editable half of the workspace document, as the save endpoint takes it. */
export function workspaceInput(settings: WorkspaceSettings): WorkspaceSettingsInput {
  const ws = settings.workspace;
  return { default_branch: ws.default_branch, report_retention_days: ws.report_retention_days, gate: { ...ws.gate } };
}

/** The editable half of the repo document, as the save endpoint takes it. */
export function repoInput(settings: RepoSettings): RepoSettingsInput {
  const repo = settings.repo;
  return {
    default_branch: repo.default_branch,
    gate: { ...repo.gate },
    ignore_paths: repo.ignore_paths,
    carryforward_parts: repo.carryforward_parts,
    public_reports: repo.public_reports,
  };
}

/** The retention selector's three windows; 0 keeps reports forever. */
export const retentionOptions: { value: number; label: string }[] = [
  { value: 90, label: "90 days" },
  { value: 365, label: "1 year" },
  { value: 0, label: "Forever" },
];
