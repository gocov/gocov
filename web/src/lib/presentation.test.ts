/**
 * @vitest-environment node
 */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { appAccount, forgeLabel, level, trend } from "./format";

// The presentation rules the server applies too — badge colours, the files
// card's "coverage changed", OIDC refusals, the dashboard's posting
// identity. internal/server/presentation_test.go checks the Go side against
// the same file, so neither side can move alone.
interface Rules {
  coverage_levels: { pct: number; level: string }[];
  deltas: { delta: number; moved: boolean }[];
  forge_names: Record<string, string>;
  app_account: string;
}

const rules = JSON.parse(
  readFileSync(fileURLToPath(new URL("../../../internal/server/testdata/presentation.json", import.meta.url)), "utf8"),
) as Rules;

test("coverage levels draw the server's badge lines", () => {
  for (const { pct, level: want } of rules.coverage_levels) expect([pct, level(pct)]).toEqual([pct, want]);
});

test("a delta moves exactly where the server's files card says it does", () => {
  for (const { delta, moved } of rules.deltas) expect([delta, trend(delta) !== "flat"]).toEqual([delta, moved]);
});

test("forge names and the app's identity are the server's", () => {
  for (const [forge, name] of Object.entries(rules.forge_names)) expect(forgeLabel(forge)).toBe(name);
  expect(appAccount).toBe(rules.app_account);
});
