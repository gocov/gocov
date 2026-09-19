/**
 * @vitest-environment node
 */
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// The shell contract with Go (internal/server/spa.go): the server serves this
// file for every page route and swaps this one tag for a page-specific title,
// meta tags and a canonical link. Reformat it, translate it or let a tool
// pretty-print it and the injection silently stops happening.
const html = readFileSync(fileURLToPath(new URL("../../index.html", import.meta.url)), "utf8");

test("index.html carries the literal title tag the server replaces", () => {
  expect(html).toContain("<title>gocov</title>");
  expect(html.split("\n").filter((line) => line.includes("<title"))).toHaveLength(1);
});

test("a build keeps it: the shell Go serves is the built file, not this one", () => {
  const built = new URL("../../../internal/webui/dist/index.html", import.meta.url);
  // Only when there is a build to look at — Go must build without one.
  if (!existsSync(built)) return;
  expect(readFileSync(fileURLToPath(built), "utf8")).toContain("<title>gocov</title>");
});
