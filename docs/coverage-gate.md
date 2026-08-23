# Coverage gate

Set the gate on a workspace's **settings** page (Defaults & coverage gate): a minimum total percentage, a minimum diff
coverage for the changed lines of PR uploads, and a max total-coverage drop. Each rule is optional — leave a field empty
to disable it — and the values apply to repos registered from then on.

- **Min coverage** — the minimum total percentage.
- **Min diff coverage** — applies to the changed lines of PR uploads (skipped when no diff coverage is available).
- **Max coverage drop** — bounds how far total coverage may fall below the latest gate-passing upload on the default
  branch; `0` forbids any drop.

Gate-failing uploads are recorded but never serve as a baseline, so re-running CI cannot launder a failure and a PR
cannot ratchet coverage down push by push. Violations mark the pushed build status FAILED and are reported in the PR
comment and the upload response (`gate` field). The uploader `gocov` CLI exits non-zero on a failed gate when run with
`-fail-on-gate`.

## Making the gate block merges

- **Bitbucket** — require the `gocov` build in the repo's merge checks; a FAILED status then blocks the PR.
- **GitHub** — add a branch protection rule under **Settings → Branches → Require status checks to pass** and pick
  `gocov` (the commit status) or `gocov coverage` (the check run).
- **GitLab** — use **Settings → Merge requests → Status checks**
  policies that reference the `gocov` commit status.

All three require the workspace to be connected to its forge — see
[Forge connections](forge-connections.md).

Note that when a commit's coverage arrives in several
[parts](parts.md), the gate is evaluated against the merged report as parts arrive, so it can fail transiently until the
last part lands — sequence the gate check after all coverage jobs.
