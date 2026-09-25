import {
  ciLabel,
  deltaText,
  duration,
  forgeLabel,
  gateSummary,
  humanBytes,
  level,
  splitPath,
  timeAgo,
  trend,
  uploaderKindLabel,
} from "./format";

test("forge names are spelled the forge's way", () => {
  expect(["github", "gitlab", "bitbucket", "gitea"].map(forgeLabel)).toEqual(["GitHub", "GitLab", "Bitbucket", "Gitea"]);
});

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

test("sizes round to whole KB and one decimal of MB", () => {
  expect([0, 999, 1 << 10, 1536, (1 << 20) - 1, 1 << 20, 3 * (1 << 20) + (1 << 19)].map(humanBytes)).toEqual([
    "0 B",
    "999 B",
    "1 KB",
    "2 KB",
    "1024 KB",
    "1.0 MB",
    "3.5 MB",
  ]);
});

test("processing time, CI service and uploader kind read as words", () => {
  expect([412, 999, 1000, 1540].map(duration)).toEqual(["412 ms", "999 ms", "1.0 s", "1.5 s"]);
  expect(["github", "gitlab", "bitbucket", "jenkins", ""].map(ciLabel)).toEqual([
    "GitHub Actions",
    "GitLab CI",
    "Bitbucket Pipelines",
    "",
    "",
  ]);
  expect(["cli", "action", "hacker"].map(uploaderKindLabel)).toEqual(["CLI", "Action", ""]);
});
