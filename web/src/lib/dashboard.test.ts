import type { AttentionItem, DashRepo } from "./api/types";
import { attentionCopy, compareRepos, repoCounts, repoMatches, visibleRepos } from "./dashboard";

const item = (over: Partial<AttentionItem> = {}): AttentionItem => ({
  kind: "failing",
  forge: "github",
  slug: "acme/api",
  name: "api",
  coverage: 41,
  min_coverage: 60,
  stale_days: null,
  ...over,
});

const repo = (over: Partial<DashRepo> = {}): DashRepo => ({
  forge: "github",
  slug: "acme/api",
  name: "api",
  coverage: 70,
  delta: null,
  gate: "pass",
  stale: false,
  series: [],
  uploaded_at: "2026-09-18T10:00:00Z",
  ...over,
});

test("a failing gate names the figure it missed", () => {
  const copy = attentionCopy(item());
  expect(copy.tone).toBe("bad");
  expect(copy.before + copy.name + copy.after).toBe("api is failing its coverage gate");
  expect(copy.message).toBe("Coverage 41.0%, below the 60% minimum.");
  expect(copy.action).toBe("Open repo");
  expect(copy.to).toBe("/repos/github/acme/api");
});

test("a failing gate without a minimum falls back to the plain sentence", () => {
  expect(attentionCopy(item({ min_coverage: null })).message).toBe("Its latest report failed the coverage gate.");
});

test("a fractional minimum keeps its digits", () => {
  expect(attentionCopy(item({ min_coverage: 82.55 })).message).toBe("Coverage 41.0%, below the 82.55% minimum.");
});

test("a stale repo counts the days since its last upload", () => {
  const copy = attentionCopy(item({ kind: "stale", stale_days: 21, min_coverage: null }));
  expect(copy.tone).toBe("warn");
  expect(copy.before + copy.name + copy.after).toBe("No uploads from api in 21 days");
  expect(copy.message).toMatch(/did not reach the upload step/);
  expect(copy.to).toBe("/repos/github/acme/api");
});

test("each filter keeps the rows it names", () => {
  const failing = repo({ gate: "fail" });
  const stale = repo({ stale: true });
  const gateless = repo({ gate: "none" });
  expect(repoMatches(failing, "all")).toBe(true);
  expect(repoMatches(failing, "failing")).toBe(true);
  expect(repoMatches(failing, "stale")).toBe(false);
  expect(repoMatches(stale, "stale")).toBe(true);
  expect(repoMatches(gateless, "nogate")).toBe(true);
  expect(repoMatches(gateless, "failing")).toBe(false);
});

test("the counts add up per condition, and a row can be in two", () => {
  const repos = [
    repo({ name: "api", gate: "fail", stale: true }),
    repo({ name: "web" }),
    repo({ name: "tools", gate: "none" }),
  ];
  expect(repoCounts(repos)).toEqual({ all: 3, failing: 1, stale: 1, nogate: 1 });
});

const names = (repos: DashRepo[]) => repos.map((r) => r.name);

test("lowest coverage leads, and a repo with no report sinks", () => {
  const repos = [
    repo({ name: "web", coverage: 82.5 }),
    repo({ name: "docs", coverage: null, uploaded_at: null }),
    repo({ name: "api", coverage: 41 }),
    repo({ name: "tools", coverage: 66 }),
  ];
  expect(names([...repos].sort(compareRepos("cov")))).toEqual(["api", "tools", "web", "docs"]);
});

test("biggest drop leads, and no baseline follows the ones that have one", () => {
  const repos = [
    repo({ name: "web", delta: 0.4 }),
    repo({ name: "tools", delta: null }),
    repo({ name: "api", delta: -2.4 }),
    repo({ name: "docs", coverage: null, delta: null }),
  ];
  expect(names([...repos].sort(compareRepos("drop")))).toEqual(["api", "web", "tools", "docs"]);
});

test("recently uploaded is newest first", () => {
  const repos = [
    repo({ name: "api", uploaded_at: "2026-09-01T00:00:00Z" }),
    repo({ name: "web", uploaded_at: "2026-09-18T00:00:00Z" }),
    repo({ name: "docs", coverage: null, uploaded_at: null }),
    repo({ name: "tools", uploaded_at: "2026-09-10T00:00:00Z" }),
  ];
  expect(names([...repos].sort(compareRepos("recent")))).toEqual(["web", "tools", "api", "docs"]);
});

test("by name is alphabetical, report or not", () => {
  const repos = [
    repo({ name: "web" }),
    repo({ name: "api" }),
    repo({ name: "aardvark", coverage: null }),
    repo({ name: "tools" }),
  ];
  expect(names([...repos].sort(compareRepos("name")))).toEqual(["api", "tools", "web", "aardvark"]);
});

test("search, filter and sort compose", () => {
  const repos = [
    repo({ name: "api", coverage: 41, gate: "fail" }),
    repo({ name: "api-docs", coverage: 90, gate: "none" }),
    repo({ name: "web", coverage: 60, gate: "fail" }),
  ];
  expect(names(visibleRepos(repos, { filter: "all", search: " API ", sort: "cov" }))).toEqual(["api", "api-docs"]);
  expect(names(visibleRepos(repos, { filter: "failing", search: "", sort: "name" }))).toEqual(["api", "web"]);
  expect(names(visibleRepos(repos, { filter: "nogate", search: "web", sort: "cov" }))).toEqual([]);
  // The input is never reordered in place.
  expect(names(repos)).toEqual(["api", "api-docs", "web"]);
});
