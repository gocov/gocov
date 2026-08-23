# gocov

![coverage](https://app.gocov.dev/badge/gocov/gocov.svg)

**Hosted:** [gocov.dev](https://gocov.dev) — public repos free forever, badge included. Or self-host the whole thing
(below); it's the same product.

Self-hostable coverage tracking — an open-source Coveralls/Codecov alternative. Single binary + Postgres.

- **Forges:** Bitbucket Cloud, GitHub and GitLab — build statuses, PR/MR comments with diff coverage, Bitbucket Code
  Insights report cards and GitHub check runs with inline annotations on uncovered changed lines
- **Formats:** Go cover profiles, LCOV (Jest, Vitest, nyc, c8), JaCoCo XML (Maven, Gradle, Android), Cobertura XML
  (coverage.py/pytest-cov, coverlet, gcovr), Clover XML (PHPUnit, Istanbul) and SimpleCov resultsets (Ruby) — detected
  from the uploaded content, no flag needed
- **Coverage gate:** per-repo minimums for total and diff coverage plus a drop tolerance; a failed gate pushes a FAILED
  status your forge's merge checks can block on
- **Web UI:** repo list, per-file coverage, line-by-line source view with hit counts, coverage trend chart, SVG badge
  per repo, sign-in with your forge account

The full tour, screenshots included, is in [docs/features.md](docs/features.md).

## Quick start

```sh
docker compose up
```

This starts Postgres and the server on http://localhost:8080 (migrations apply automatically). Then, in the web UI:

1. [Enable sign-in](docs/sign-in.md) with your forge and sign in — the onboarding wizard registers your workspace
   (org/group/user) and shows its upload token, once. A brand-new instance tracks no workspaces yet, so set
   `GOCOV_ALLOWED_WORKSPACES` to the one you want to track (e.g. `GOCOV_ALLOWED_WORKSPACES=myorg`) or every sign-in is
   denied.
2. Set `GOCOV_TOKEN` (secured) and `GOCOV_SERVER` as workspace variables in CI; repos register themselves on their first
   upload.
3. Optionally [connect the workspace to its forge](docs/forge-connections.md)
   — one click — for statuses, PR comments, check runs and diff coverage.

Upload from CI (GitHub Actions shown; [Bitbucket Pipelines and GitLab CI here](docs/ci-upload.md)):

```yaml
- run: npx jest --coverage        # or go test -coverprofile, mvn verify, pytest --cov, ...
- uses: gocov/gocov-action@v1
  with:
    files: coverage/lcov.info
    token: ${{ secrets.GOCOV_TOKEN }}
```

The action downloads the CLI binary and checks its sha256, so there is no toolchain to install — the same three lines
work whatever your tests are written in. Every supported format uploads this way; point `files` at whatever your test
runner produced. The badge is one line of markdown:

```markdown
![coverage](https://gocov.example/badge/myworkspace/myrepo.svg)
```

## Documentation

Everything else lives in [docs/](docs/README.md):

- [Getting started](docs/getting-started.md) — onboarding, workspace settings, hosted mode
- [Sign-in](docs/sign-in.md) — OAuth setup for Bitbucket, GitHub, GitLab and the access model
- [Forge connections](docs/forge-connections.md) — GitHub App and Bitbucket/GitLab one-click connect
- [Uploading from CI](docs/ci-upload.md) — all three CIs, prebuilt binaries, other ecosystems
- [Coverage gate](docs/coverage-gate.md) · [Parts](docs/parts.md) ·
  [API & badge](docs/api.md) · [Configuration](docs/configuration.md) ·
  [Development](docs/development.md)

## License

AGPL-3.0 — see [LICENSE](LICENSE).
