// Pure helpers the two settings pages share: what a gate says about
// itself, what the ignore box holds, and whether a form still matches the
// document it was seeded from. No React, no fetching — every one of these
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

export function sameGate(a: Gate, b: Gate): boolean {
  return (
    a.min_coverage === b.min_coverage &&
    a.min_diff_coverage === b.min_diff_coverage &&
    a.max_coverage_drop === b.max_coverage_drop
  );
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
    public_reports: repo.public_reports,
  };
}

/** Has the form moved away from what was saved? Drives the Save button. */
export function workspaceDirty(form: WorkspaceSettingsInput, saved: WorkspaceSettingsInput): boolean {
  return (
    form.default_branch !== saved.default_branch ||
    form.report_retention_days !== saved.report_retention_days ||
    !sameGate(form.gate, saved.gate)
  );
}

export function repoDirty(form: RepoSettingsInput, saved: RepoSettingsInput): boolean {
  return (
    form.default_branch !== saved.default_branch ||
    form.ignore_paths !== saved.ignore_paths ||
    form.public_reports !== saved.public_reports ||
    !sameGate(form.gate, saved.gate)
  );
}

/** The retention selector's three windows; 0 keeps reports forever. */
export const retentionOptions: { value: number; label: string }[] = [
  { value: 90, label: "90 days" },
  { value: 365, label: "1 year" },
  { value: 0, label: "Forever" },
];
