import { deltaText, gateSummary, level, splitPath, timeAgo, trend } from "./format";

test("coverage thresholds", () => {
  expect([49.9, 50, 75, 75.1].map(level)).toEqual(["bad", "warn", "warn", "good"]);
});

test("a delta under 0.05 reads as flat", () => {
  expect(trend(0.04)).toBe("flat");
  expect(deltaText(0.04)).toBe("0.0%");
  expect(deltaText(1.23)).toBe("+1.2%");
  expect(deltaText(-6.4)).toBe("−6.4%");
});

test("relative time matches the server's wording", () => {
  const now = new Date("2026-09-19T12:00:00Z");
  expect(timeAgo("2026-09-19T11:59:40Z", now)).toBe("just now");
  expect(timeAgo("2026-09-19T11:00:00Z", now)).toBe("1 hour ago");
  expect(timeAgo("2026-09-18T11:00:00Z", now)).toBe("yesterday");
  expect(timeAgo("2026-08-30T11:00:00Z", now)).toBe("2026-08-30");
});

test("gate summary and path split", () => {
  expect(gateSummary({ min_coverage: 80, min_diff_coverage: null, max_coverage_drop: 1.5 })).toBe("total ≥ 80%, drop ≤ 1.5%");
  expect(splitPath("internal/server/api.go")).toEqual(["internal/server/", "api.go"]);
  expect(splitPath("main.go")).toEqual(["", "main.go"]);
});
