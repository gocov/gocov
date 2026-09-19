// The dashboard's words and orderings, kept out of the components so they
// can be read at a glance and tested on their own. The sentences, and the
// filter and sort semantics, are the ones the Go dashboard this replaced had
// (collectAttention in internal/server/dashboard.go, and its inline script).

import type { AttentionItem, DashRepo } from "./api/types";
import { pct, plural } from "./format";
import { routes } from "./urls";

export type AttentionTone = "bad" | "warn";

/** One needs-attention notice as sentences: the name is set in monospace, so it stays its own part. */
export interface AttentionCopy {
  tone: AttentionTone;
  /** The condition, for a screen reader — the dot alone must not carry it. */
  status: string;
  before: string;
  name: string;
  after: string;
  message: string;
  action: string;
  /** In-app route the action opens. */
  to: string;
}

/** A notice ready to list: its sentences plus a stable key. */
export interface AttentionRow extends AttentionCopy {
  key: string;
}

/**
 * The notices as the dashboard lists them: things that happened — a gate
 * failed, uploads stopped. A repository without a gate is deliberately not
 * one of them (the server does not send it): it is a standing choice, and a
 * section that is always there stops being read. The table's row says "Set a
 * gate" and the No gate filter counts it.
 */
export function attentionRows(items: AttentionItem[]): AttentionRow[] {
  return items.map((item) => ({ ...attentionCopy(item), key: `${item.kind}:${item.forge}/${item.slug}` }));
}

/** Go's %.4g for a threshold: 60 reads "60", 82.55 stays "82.55". */
function threshold(v: number): string {
  const s = v.toPrecision(4);
  return s.includes(".") ? s.replace(/\.?0+$/, "") : s;
}

export function attentionCopy(item: AttentionItem): AttentionCopy {
  switch (item.kind) {
    case "failing":
      return {
        tone: "bad",
        status: "Failing",
        before: "",
        name: item.name,
        after: " is failing its coverage gate",
        message:
          item.min_coverage === null || item.coverage === null
            ? "Its latest report failed the coverage gate."
            : `Coverage ${pct(item.coverage)}, below the ${threshold(item.min_coverage)}% minimum.`,
        action: "Open repo",
        to: routes.repo(item.forge, item.slug),
      };
    case "stale":
      return {
        tone: "warn",
        status: "Stale",
        before: "No uploads from ",
        name: item.name,
        after: ` in ${plural(item.stale_days ?? 0, "day")}`,
        message: "Its last pipeline run did not reach the upload step; the coverage shown is stale.",
        action: "Open repo",
        to: routes.repo(item.forge, item.slug),
      };
  }
}

export type RepoFilter = "all" | "failing" | "stale" | "nogate";
export type RepoSort = "cov" | "drop" | "recent" | "name";

/** The filter tabs, in the order they are shown. */
export const repoFilters: RepoFilter[] = ["all", "failing", "stale", "nogate"];

export function repoMatches(repo: DashRepo, filter: RepoFilter): boolean {
  switch (filter) {
    case "failing":
      return repo.gate === "fail";
    case "stale":
      return repo.stale;
    case "nogate":
      return repo.gate === "none";
    case "all":
      return true;
  }
}

/** How many rows each tab would keep — the counts beside the filter labels. */
export function repoCounts(repos: DashRepo[]): Record<RepoFilter, number> {
  const counts: Record<RepoFilter, number> = { all: repos.length, failing: 0, stale: 0, nogate: 0 };
  for (const repo of repos) {
    if (repo.gate === "fail") counts.failing++;
    if (repo.stale) counts.stale++;
    if (repo.gate === "none") counts.nogate++;
  }
  return counts;
}

const uploadedAt = (repo: DashRepo) => (repo.uploaded_at === null ? 0 : Date.parse(repo.uploaded_at));

/**
 * The row order for a sort mode. A repo with no report at all sinks to the
 * bottom whichever mode is chosen: it has nothing to compare.
 */
export function compareRepos(sort: RepoSort): (a: DashRepo, b: DashRepo) => number {
  return (a, b) => {
    const reported = a.coverage !== null;
    if (reported !== (b.coverage !== null)) return reported ? -1 : 1;
    switch (sort) {
      case "drop": {
        // Most negative first; no baseline means no drop to rank, so it follows.
        if (a.delta === null && b.delta === null) return 0;
        if (a.delta === null) return 1;
        if (b.delta === null) return -1;
        return a.delta - b.delta;
      }
      case "recent":
        return uploadedAt(b) - uploadedAt(a);
      case "name":
        return a.name.localeCompare(b.name);
      case "cov":
        return (a.coverage ?? 0) - (b.coverage ?? 0);
    }
  };
}

/** Filter, search and sort in one pass — all three are instant and client-side. */
export function visibleRepos(
  repos: DashRepo[],
  { filter, search, sort }: { filter: RepoFilter; search: string; sort: RepoSort },
): DashRepo[] {
  const q = search.trim().toLowerCase();
  return repos
    .filter((repo) => repoMatches(repo, filter) && (q === "" || repo.name.toLowerCase().includes(q)))
    .sort(compareRepos(sort));
}
