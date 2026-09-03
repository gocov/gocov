# Ignoring files

Some files belong in the repository but not in the coverage number: generated code, mocks, a dev harness under
`cmd/`, vendored sources. Ignore patterns leave them out of every report before totals, diff coverage, the
[gate](coverage-gate.md) and the badge are computed — as if the coverage tool had never seen them.

## Where to set them

- **Repository settings → Ignored files** — one pattern per line. This is the usual place: it applies to every
  upload for the repo, whichever CI job sends it, and it is visible to the whole team.
- **The upload itself** — the CLI's [`-ignore`](cli.md) flag (or `$GOCOV_IGNORE`), the `ignore` input of the
  [GitHub Action](github-actions.md#options), and the `ignore` part of the [raw API](api.md). Handy when one job
  needs an extra exclusion, or when you want the patterns versioned next to the CI config. Upload patterns are
  applied on top of the repo's, never instead of them.

```sh
gocov upload -ignore 'cmd/preview/**' -ignore '*_mock.go' coverage.out
```

Patterns take effect on uploads received from then on. Reports already stored keep their numbers.

## Pattern syntax

Patterns are globs in the `.gitignore` spirit, matched against the paths shown in reports:

| pattern              | matches                                                                        |
|----------------------|--------------------------------------------------------------------------------|
| `cmd/preview/**`     | everything under a `cmd/preview/` directory                                    |
| `cmd/preview`        | the same — a pattern that matches a directory covers everything under it       |
| `**/*.pb.go`         | protobuf output at any depth                                                   |
| `*_mock.go`          | a bare name matches anywhere                                                   |
| `internal/*/gen/*`   | `*` stays within one directory: `internal/api/gen/x.go`, not `internal/a/b/gen/x.go` |
| `src/**/*.test.ts`   | `**` crosses directories                                                        |
| `/cmd/preview`       | a leading `/` pins the pattern to the root: not `tools/cmd/preview`            |

A pattern matches at any directory level unless it starts with `/`. Coverage reports often record paths under a
root the pattern never mentions — a Go module path, the CI checkout directory, an absolute path — and floating is
what lets a pattern written against the repository tree still land. `?` matches a single character, `**` is only
special as a whole segment (`**.go` is read as `**/*.go`), and a trailing `/` names a directory, so a line copied
from a `.gitignore` usually works as is.

## Paths and prefixes

Patterns are tried against the path as the coverage report spells it, and — when the upload has a
[path prefix](cli.md#-path-prefix) — against the path with that prefix removed, so that an anchored pattern
written against the repository tree matches too. Go profiles record module-qualified paths
(`example.com/mod/cmd/preview/main.go`) and the CLI fills the prefix from `go.mod`, so `/cmd/preview/**` matches
without spelling out the module. JaCoCo's package paths and lcov's checkout-relative or absolute paths are matched
as they are.

## What ignoring does not do

- **The raw profile is stored untouched.** Ignoring shapes the numbers, not the upload; a pattern that matches too
  much can be corrected and the next upload is right again. A pattern set that matches *every* file is refused at
  upload time (`ignore patterns matched every file in the report`) rather than landing a 0% report.
- **Nothing is recalculated.** A commit uploaded before the pattern was set keeps its report, and the
  [delta](coverage-changed.md) on the first upload after a change in patterns compares against that older baseline.
- **Diff coverage skips ignored files too.** Changed lines in an ignored file neither count for nor against the PR.

The upload's page and the CLI output say how many files a pattern dropped (`ignored: 3 files`), which is the quickest
way to check a new pattern does what you meant.
