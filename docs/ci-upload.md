# Uploading coverage from CI

## Bitbucket Pipelines

Use the [gocov pipe](https://bitbucket.org/gocov/upload-pipe) (commit, branch, repo and PR id are auto-detected):

```yaml
- step:
    script:
      - go test ./... -covermode=atomic -coverprofile=coverage.out
      - pipe: docker://gocov/upload-pipe:0
        variables:
          FILES: coverage.out
          TOKEN: $GOCOV_TOKEN
```

with `GOCOV_TOKEN` set as a secured repository variable (add
`SERVER: $GOCOV_SERVER` when self-hosting). On runners without Docker, run the CLI directly instead:

```yaml
- step:
    script:
      - go test ./... -covermode=atomic -coverprofile=coverage.out
      - go run github.com/gocov/gocov/cmd/gocov@latest upload coverage.out
```

with `GOCOV_SERVER` and `GOCOV_TOKEN` set as repository variables.

## GitHub Actions

Use [`gocov/gocov-action`](https://github.com/marketplace/actions/gocov-coverage-upload). It downloads the pinned CLI
binary for the runner and verifies its checksum, so **no Go toolchain is needed** — the same three lines work whatever
your tests are written in. Commit, branch, repo and PR number are auto-detected, including the PR head SHA on
`pull_request` runs.

```yaml
# Go
- run: go test ./... -covermode=atomic -coverprofile=coverage.out
- uses: gocov/gocov-action@v1
  with:
    files: coverage.out
    token: ${{ secrets.GOCOV_TOKEN }}
```

```yaml
# JavaScript/TypeScript — Jest, Vitest, nyc, c8
- run: npx jest --coverage
- uses: gocov/gocov-action@v1
  with:
    files: coverage/lcov.info
    token: ${{ secrets.GOCOV_TOKEN }}
```

```yaml
# Java/Kotlin — Maven with the jacoco-maven-plugin
- run: mvn verify
- uses: gocov/gocov-action@v1
  with:
    files: target/site/jacoco/jacoco.xml
    token: ${{ secrets.GOCOV_TOKEN }}
```

`files` takes a comma-separated list and globs. When self-hosting, add `server: https://gocov.example`; the default is
the hosted service. `part:` labels one slice of a matrix build — see [Parts](parts.md).

On a runner that already has Go, the CLI also runs straight from source, no action involved:

```yaml
- run: go run github.com/gocov/gocov/cmd/gocov@latest upload coverage.out
  env:
    GOCOV_SERVER: ${{ vars.GOCOV_SERVER }}
    GOCOV_TOKEN: ${{ secrets.GOCOV_TOKEN }}
```

## GitLab CI

Project path, commit, branch and MR iid are auto-detected, including the real head SHA on merged-results pipelines:

```yaml
workflow:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH

coverage:
  image: golang:1.23
  script:
    - go test ./... -covermode=atomic -coverprofile=coverage.out
    - curl -fsSLO https://github.com/gocov/gocov/releases/download/v0.11.0/gocov-linux-amd64
    - curl -fsSL https://github.com/gocov/gocov/releases/download/v0.11.0/checksums.txt
      | grep ' gocov-linux-amd64$' | sha256sum -c -
    - chmod +x gocov-linux-amd64
    - ./gocov-linux-amd64 upload coverage.out
```

with `GOCOV_TOKEN` (masked, but **not protected** — GitLab checks
"Protect variable" by default, and protected variables never reach merge request pipelines) and, when self-hosting,
`GOCOV_SERVER` set as CI/CD variables under **Settings → CI/CD → Variables** on the group or project. The `workflow`
rules run the job on merge requests and the default branch without duplicate pipelines. Note that gitlab.com's free tier
requires a verified account (credit card) before shared runners pick up jobs.

## Prebuilt binaries

On runners without a Go toolchain, use the prebuilt binaries from
[GitHub Releases](https://github.com/gocov/gocov/releases) instead (linux/darwin/windows, amd64 + arm64, checksums
included). Pin a version and cache the download on self-hosted runners:

```sh
ver=v0.11.0
arch=$(uname -m); case "$arch" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; esac
bin="$HOME/.cache/gocov/gocov-$ver-linux-$arch"
if [ ! -x "$bin" ]; then
  mkdir -p "$(dirname "$bin")"
  curl -fsSL "https://github.com/gocov/gocov/releases/download/$ver/gocov-linux-$arch" -o "$bin"
  chmod +x "$bin"
fi
"$bin" upload coverage.out
```

## Outside CI

Values fall back to git or can be passed explicitly:

```sh
gocov upload -server https://gocov.example -token $TOKEN \
  -repo myworkspace/myrepo -commit $(git rev-parse HEAD) -branch main \
  coverage.out
```

## Other ecosystems

Other ecosystems upload their reports the same way — the format is detected from the content, no flag needed:

```sh
npx jest --coverage             # or vitest run --coverage, nyc, c8 ...
gocov upload coverage/lcov.info

mvn verify                      # with the jacoco-maven-plugin
gocov upload target/site/jacoco/jacoco.xml

gradle test jacocoTestReport    # xml.required = true
gocov upload build/reports/jacoco/test/jacocoTestReport.xml

pytest --cov --cov-report=xml   # coverage.py / pytest-cov
gocov upload coverage.xml

phpunit --coverage-clover clover.xml
gocov upload clover.xml

bundle exec rspec               # with simplecov enabled
gocov upload coverage/.resultset.json
```

JaCoCo paths are package-qualified (`com/example/Foo.java`); diff coverage matches them against repo paths by suffix, so
source roots like
`src/main/java` need no configuration.

## See also

- [Parts](parts.md) — combining several uploads (backend, frontend, e2e, matrix jobs) into one report per commit
- [API & badge](api.md) — the raw upload endpoint, for anything the CLI doesn't cover
