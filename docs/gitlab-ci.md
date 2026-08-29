# GitLab CI

1. Add `GOCOV_TOKEN` under **Settings → CI/CD → Variables** on the group or project, holding your workspace's
   [upload token](getting-started.md). Mark it **masked**, but **not protected** — GitLab checks "Protect variable" by
   default, and protected variables never reach merge request pipelines. When self-hosting, add `GOCOV_SERVER` beside
   it.

2. Add an upload step after your tests:

<!-- x-release-please-start-version -->
```yaml
workflow:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH

coverage:
  image: golang:1.23
  script:
    - go test ./... -covermode=atomic -coverprofile=coverage.out
    - curl -fsSLO https://github.com/gocov/gocov/releases/download/v0.13.0/gocov-linux-amd64
    - curl -fsSL https://github.com/gocov/gocov/releases/download/v0.13.0/checksums.txt
      | grep ' gocov-linux-amd64$' | sha256sum -c -
    - chmod +x gocov-linux-amd64
    - ./gocov-linux-amd64 upload coverage.out
```
<!-- x-release-please-end -->

Only the test command and the uploaded path change for other languages —
[Languages & formats](languages.md) lists what each test tool writes.

Project path, commit, branch and MR iid are auto-detected, including the real head SHA on merged-results pipelines.
The `workflow` rules run the job on merge requests and the default branch without duplicate pipelines.

Note that gitlab.com's free tier requires a verified account (credit card) before shared runners pick up jobs.

## Blocking merges on coverage

With the workspace [connected](connecting.md), every upload sets a commit status and posts a diff-coverage note on the
merge request. To make the [coverage gate](coverage-gate.md) block merges, reference the `gocov` commit status under
**Settings → Merge requests → Status checks**.
