# gocov

![coverage](https://app.gocov.dev/badge/gocov/gocov.svg)

Self-hostable coverage tracking — an open-source Coveralls/Codecov
alternative. Single binary + Postgres. Supported forges: Bitbucket Cloud,
GitHub and GitLab. Supported formats: Go cover profiles, LCOV tracefiles
(JavaScript/TypeScript — Jest, Vitest, nyc, c8), JaCoCo XML
(Java/Kotlin — Maven, Gradle, Android), Cobertura XML
(Python — coverage.py/pytest-cov; also coverlet, gcovr), Clover XML
(PHPUnit, Istanbul) and SimpleCov resultsets (Ruby); the format is
detected from the uploaded content.

## Features (MVP)

- Parses Go cover profiles (`go test -coverprofile`), LCOV tracefiles
  (Jest/Vitest/nyc `lcov.info`), JaCoCo XML (`jacoco.xml`), Cobertura
  XML (`coverage.xml`), Clover XML (PHPUnit `clover.xml`) and SimpleCov
  resultsets (`.resultset.json`) into total and per-file coverage
- `POST /api/v1/upload` API with per-repo Bearer tokens
- SVG coverage badge per repo (`/badge/{workspace}/{repo}.svg`)
- Web UI: repo list → upload list → per-file coverage table
- Uploader CLI that auto-detects Bitbucket Pipelines, GitHub Actions
  and GitLab CI environment variables and falls back to git
- Pushes a `coverage: X% (±Y%)` build status to Bitbucket commits (or a
  commit status to GitHub/GitLab) when the repo's workspace is connected
  to its forge
- Coverage gate: per-repo minimums for total and diff coverage plus a
  drop tolerance; violations push a FAILED build status, so a Bitbucket
  merge check can block the PR
- Source view: any file in an upload renders line by line with coverage
  overlay and hit counts, fetched from the forge at the exact commit and
  cached immutably (misses are cached too); without a forge connection
  the page falls back to an uncovered-line summary. When an upload has
  no `path_prefix`, recorded paths that carry an unmapped leading
  prefix (a Go module path, a CI checkout directory) are resolved by
  probing trimmed variants against the forge
- Web UI sign-in with Bitbucket, GitHub and/or GitLab: configure an OAuth
  consumer/app and every page requires login, allowed only for members
  of the workspaces and orgs the instance tracks (see "Enable
  sign-in"). Uploads, badges and health checks are unaffected; no
  passwords are ever stored
- Diff coverage for pull requests: fetches the PR diff from the forge,
  intersects changed lines with coverage blocks, and posts a PR comment
  listing uncovered changed lines — repeated uploads update the same
  comment instead of stacking new ones. Works on Bitbucket, GitHub and
  GitLab alike (on GitLab as a merge request note; GitLab has no
  check-run/Code-Insights equivalent, so the note's diff coverage table
  is the in-MR surface)
- Coverage inside the PR, via Bitbucket Code Insights: every upload
  attaches a report card to its commit (total coverage, delta, diff
  coverage, gate verdict) that Bitbucket shows in the pull request's
  Reports panel, and PR uploads annotate uncovered changed lines right
  in the diff view — reviewers see untested code exactly where they are
  reviewing it, with no plugin on the Bitbucket side. Changed files with
  no coverage data at all get a file-level marker, and the report card
  lists the worst-covered changed files while the field budget allows.
  Re-uploads replace the report and annotations in place; no coverage
  product on Bitbucket Cloud ships this today

  _[screenshot: coverage report card and inline annotations in a
  Bitbucket PR]_

- On GitHub the same surface ships as a **check run** named `gocov
  coverage`: a summary card with the identical data, a conclusion that
  mirrors the coverage gate (success/failure, neutral without a gate) so
  branch protection can require it, and inline annotations on uncovered
  changed lines in the Files changed view. Re-uploads replace the run in
  place. Writing check runs needs a credential GitHub allows to (see
  "GitHub token permissions"); with other tokens the check run is
  skipped while status, comment and gate keep working

- Coverage trend chart: the repo page graphs total coverage over the
  branch's recent uploads (gate failures marked in red, every point
  links to its upload) — rendered as inline SVG on the server, no
  JavaScript chart library

  _[screenshot: coverage trend chart on a repo page]_

The architecture is deliberately extensible: coverage formats sit behind
`profile.Parser`, forges behind `forge.Forge`, raw profile storage behind
`blobstore.Store`, and the database schema stores a format-agnostic
normalized model — GitHub and GitLab were each added this way, and new
formats or S3 storage slot in without rewrites.

## Quick start

```sh
docker compose up
```

This starts Postgres and the server on http://localhost:8080 (migrations
apply automatically).

### Onboarding a whole workspace (recommended)

For many repos, register the workspace once and use a single token:

```sh
docker compose exec server gocov-server workspace add \
  -prefix myworkspace -default-branch main
```

Set the printed token as a *Bitbucket workspace variable* (`GOCOV_TOKEN`,
secured) together with `GOCOV_SERVER` — every repo inherits them. Repos
register themselves on their first upload; their default branch is asked
from Bitbucket when the workspace has credentials of its own (or a
one-click connection), falling back to the workspace's `-default-branch`.

### Registering repos one by one

```sh
docker compose exec server gocov-server repo add \
  -slug myworkspace/myrepo \
  -default-branch main
```

For a GitHub repo, the slug is `owner/repo`; for a GitLab project it is
the full namespace path, subgroups included:

```sh
docker compose exec server gocov-server repo add \
  -slug myorg/myrepo -forge github -default-branch main

docker compose exec server gocov-server repo add \
  -slug mygroup/subgroup/myproject -forge gitlab -default-branch main
```

Registering a repo only tracks coverage. To let gocov post build
statuses, PR/MR comments and check runs, connect the repo's workspace
once through the web UI (GitHub App, or a Bitbucket/GitLab one-click
grant — see "Forge connection" below). A repo whose workspace has no
connection still stores and reports coverage; the forge surfaces are
simply skipped.

A GitHub org or GitLab group can also be onboarded wholesale:
`workspace add -prefix myorg -forge github` (or `-prefix
mygroup/subgroup -forge gitlab` — a workspace can sit at any level of
the namespace tree) — repos then register themselves on first upload,
exactly like a Bitbucket workspace.

Manage repos later with:

```sh
gocov-server repo list                                   # slugs, branches, gate
gocov-server repo rotate-token -slug myworkspace/myrepo  # invalidates the old token
gocov-server repo update -slug myworkspace/myrepo \
  -default-branch develop                                # and/or gate flags
gocov-server repo remove -slug myworkspace/myrepo -force # deletes uploads and raw profiles too;
                                                         # without -force only prints a summary
gocov-server workspace list|rotate-token|update|remove   # workspace token management
```

### Enable sign-in (Bitbucket, GitHub and/or GitLab)

Out of the box the web UI is open and shows a banner saying so — nothing
changes on upgrade until you opt in. Configure one or both providers;
each renders as its own button on the login page.

For **Bitbucket**:

1. Create an OAuth consumer under **Workspace settings →
   OAuth consumers → Add consumer** with
   - **Callback URL**: `https://your-gocov-host/oauth/bitbucket/callback`
     (must be exactly `GOCOV_BASE_URL` + `/oauth/bitbucket/callback`)
   - **Permissions**: *Account: Read* and *Email* only — nothing broader
     is needed
2. Set the consumer's key and secret on the server:

```sh
GOCOV_OAUTH_BITBUCKET_KEY=...
GOCOV_OAUTH_BITBUCKET_SECRET=...
```

For **GitHub**:

1. Create an OAuth app under **Settings → Developer settings → OAuth
   Apps → New OAuth App** (on your account or org) with
   - **Authorization callback URL**:
     `https://your-gocov-host/oauth/github/callback`
2. Set the app's client id and secret on the server:

```sh
GOCOV_OAUTH_GITHUB_KEY=...
GOCOV_OAUTH_GITHUB_SECRET=...
```

gocov requests the read-only `read:org` and `user:email` scopes at
login. Note that org members may need to grant/request the app's access
to the org once (GitHub's third-party application policy) for the org
to appear in their membership.

For **GitLab**:

1. Create an OAuth application under **Preferences → Applications** (or
   on a group/instance) with
   - **Redirect URI**: `https://your-gocov-host/oauth/gitlab/callback`
   - **Scopes**: `read_user` and `read_api` only — nothing broader is
     needed
2. Set the application's id and secret on the server:

```sh
GOCOV_OAUTH_GITLAB_KEY=...
GOCOV_OAUTH_GITLAB_SECRET=...
```

Membership is derived from the account's groups — subgroups included,
each by its full path (`group/subgroup`) — plus the username itself, so
user-namespace projects admit their owner.

From then on every UI page requires signing in. Access is decided at
login time by membership: by default, members of any workspace/org the
instance tracks (registered workspaces and the workspace part of
registered repo slugs) may sign in, and everyone else gets a clear
denial page; on GitHub the account's own username also counts, so
user-namespace repos admit their owner. Set `GOCOV_ALLOWED_WORKSPACES`
(comma-separated workspace/org slugs) to replace the derived set with
an explicit list. Accounts are provisioned on first successful sign-in
— there is no user bookkeeping, and gocov never sees or stores
passwords (the forge tokens are discarded right after login).

CI is unaffected either way: the upload API keeps its Bearer tokens,
badges stay embeddable, `/healthz` stays open.

### Hosted mode (self-service signup)

`GOCOV_MODE=hosted` turns the instance into a self-service one: any
forge account may sign in, and a user who belongs to no tracked
workspace lands on **/register**, which lists the workspaces their
forge account is a member of (captured at sign-in). Claiming one
creates the workspace, makes the user a member and shows the upload
token — once; afterwards it can only be rotated. Only workspaces the
forge itself reports for the account can be registered, so there is
nothing to dispute: if a colleague registered your workspace first,
signing in simply makes you a member.

Registration lands on an onboarding page: the forge-appropriate CI
snippet with the server URL and token pre-filled, and a live "waiting
for your first upload" state that flips to the repo link once coverage
arrives.

The default (`GOCOV_MODE=private`) keeps exactly the behavior described
above — self-hosted deployments upgrade with zero change. Hosted mode
requires at least one sign-in provider.

### Workspace settings in the UI

Signed-in members manage their workspaces from the dashboard (private
and hosted mode alike): rotate the upload token (the old one dies
immediately; the new one is shown once), change the default branch and
gate defaults for auto-registered repos, and connect the workspace to
its forge (GitHub App, or a Bitbucket/GitLab one-click grant) for
statuses, PR comments and insights. The upload token is never rendered
back — it is shown once, right after a rotation. The CLI (`gocov-server
workspace ...`) keeps working; the UI is an addition, not a migration.

```sh
gocov-server user list                          # who has signed in
gocov-server user remove -email jane@example.com  # revoke immediately
```

Removal deletes the account and its sessions; the person can sign in
again (and is re-provisioned) as long as they are still a workspace
member. Sessions last 30 days; membership is re-checked at each login,
not per request.

Access mirrors your forge workspace membership. Once sign-in is
configured, each account sees only the repos in the workspaces and orgs
the forge says it belongs to — the repo list is filtered, and a direct
link to another workspace's repo, upload or source page returns 404.
Memberships are synced from the forge on every sign-in, so there is no
separate invite or member-management step: add someone to the workspace
on the forge and they see its coverage at their next login;
remove them and it disappears. A single-team self-host where everyone
belongs to the same workspace is unaffected, as is an instance with
sign-in left open — both stay exactly as before.

### GitHub App (one-click connect)

Instead of manufacturing a token, GitHub workspaces can install a
GitHub App: one click on GitHub and statuses, PR comments and check
runs work with zero credential entry, authored by the app's bot
identity (e.g. `gocov[bot]`). The App is also the first-class Checks
API citizen, so check runs stop being permission-fragile.

To run one on your own deployment:

1. Register a GitHub App (**Settings → Developer settings → GitHub
   Apps → New GitHub App**) with
   - **Setup URL**: `https://your-gocov-host/github/setup`, with
     *Redirect on update* enabled
   - **Webhook**: optional. gocov's model is upload-driven, so a
     self-hosted app can leave it disabled — installs are linked
     through the setup redirect and uninstalls are detected lazily. A
     **Marketplace listing requires it**: set the webhook URL to
     `https://your-gocov-host/github/webhook`, a webhook secret, and
     `GOCOV_GITHUB_WEBHOOK_SECRET` to the same value on the server. The
     endpoint verifies each delivery's signature; it logs
     `marketplace_purchase` events and flips a workspace's app-broken
     flag on `installation` deleted/suspend/unsuspend
   - **Repository permissions**: *Checks: Read & write*, *Commit
     statuses: Read & write*, *Pull requests: Read & write*,
     *Contents: Read-only*, *Metadata: Read-only*
   - **Organization permissions**: *Members: Read-only* (org
     membership for sign-in sync)
2. Generate a private key on the app page and set both variables on
   the server:

```sh
GOCOV_GITHUB_APP_ID=...
GOCOV_GITHUB_APP_PRIVATE_KEY=/path/to/gocov.private-key.pem  # or the PEM content itself
```

Members then connect from the workspace settings or setup page
("Install the gocov app"); after GitHub's install screen they land back
on gocov with the workspace connected. In hosted mode the install can
even come first — an install on an account with no workspace yet
registers it on the spot (same claim rules as **/register**).

A connected installation is what gives the workspace's repos forge
access. Uninstalling the app on GitHub is detected on the next upload —
the affected surfaces degrade to `skipped`, never to a failed upload —
and the settings page offers a reconnect.

### Bitbucket workspace connect (one-click)

Bitbucket workspaces get the same effortless path: a member clicks
**Connect workspace** on the settings (or setup) page, consents once on
Bitbucket, and statuses, PR comments, reports, diffs and source fetch
work from then on. To enable it, the deployment needs the sign-in OAuth
consumer plus an encryption key:

```sh
GOCOV_SECRET_KEY=...   # 64 hex characters (`openssl rand -hex 32`); encrypts the stored grant at rest
```

The AES key is derived from this value with a plain SHA-256, so the
value itself must carry the full 256 bits of entropy. The server
requires exactly 64 hex characters and refuses to boot on anything
else — generate it with `openssl rand -hex 32` rather than inventing a
passphrase.

and the consumer's permissions extended beyond sign-in: **Account:
Read**, **Email**, **Repositories: Write**, **Pull requests: Write**.
(Bitbucket scopes live on the consumer, not the consent request, so the
sign-in consent lists them too — sign-in itself still stores no forge
tokens.)

Honest caveat, stated in the UI at connect time: Bitbucket has no app
identity, so posts appear as the account that clicked Connect. Teams
with a bot account should sign the bot in and connect with it.

The grant's refresh token is stored on the workspace, AES-GCM-encrypted
under `GOCOV_SECRET_KEY`; access tokens live only in memory. Bitbucket
rotates refresh tokens on every use — gocov persists each rotation
atomically. If the grant dies (the connecting account leaves the
workspace, the consent is revoked under *Personal settings → Authorized
applications*, or the token ages out after three unused months), the
next upload degrades to `skipped`, never to a failure, and the settings
page offers a reconnect.

### Coverage gate

```sh
gocov-server repo update -slug myworkspace/myrepo \
  -min-coverage 80 -min-diff-coverage 70 -max-drop 0.5
```

Each rule is optional: `-min-coverage` is the minimum total percentage,
`-min-diff-coverage` applies to the changed lines of PR uploads (skipped
when no diff coverage is available), and `-max-drop` bounds how far
total coverage may fall below the latest gate-passing upload on the
default branch (0 forbids any drop). Gate-failing uploads are recorded
but never serve as a baseline, so re-running CI cannot launder a failure
and a PR cannot ratchet coverage down push by push. Violations mark the
pushed build status FAILED — require the `gocov` build in Bitbucket's
merge checks to block such PRs — and are reported in the PR comment and
the upload response (`gate` field). `-clear-gate` removes all rules. The
same flags on `workspace add` and `workspace update` set defaults
inherited by auto-registered repos. The CLI exits non-zero on a failed
gate when run with `-fail-on-gate`.

## Uploading coverage from CI

In Bitbucket Pipelines, use the
[gocov pipe](https://bitbucket.org/gocov/upload-pipe) (commit, branch,
repo and PR id are auto-detected):

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
`SERVER: $GOCOV_SERVER` when self-hosting). On runners without Docker,
run the CLI directly instead:

```yaml
- step:
    script:
      - go test ./... -covermode=atomic -coverprofile=coverage.out
      - go run github.com/gocov/gocov/cmd/gocov@latest upload coverage.out
```

with `GOCOV_SERVER` and `GOCOV_TOKEN` set as repository variables.

In GitHub Actions (commit, branch, repo and PR number are auto-detected,
including the PR head SHA on `pull_request` runs):

```yaml
- run: go test ./... -covermode=atomic -coverprofile=coverage.out
- run: go run github.com/gocov/gocov/cmd/gocov@latest upload coverage.out
  env:
    GOCOV_SERVER: ${{ vars.GOCOV_SERVER }}
    GOCOV_TOKEN: ${{ secrets.GOCOV_TOKEN }}
```

In GitLab CI (project path, commit, branch and MR iid are auto-detected,
including the real head SHA on merged-results pipelines):

```yaml
workflow:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH

coverage:
  image: golang:1.23
  script:
    - go test ./... -covermode=atomic -coverprofile=coverage.out
    - curl -fsSLO https://github.com/gocov/gocov/releases/download/v0.10.0/gocov-linux-amd64
    - curl -fsSL https://github.com/gocov/gocov/releases/download/v0.10.0/checksums.txt
      | grep ' gocov-linux-amd64$' | sha256sum -c -
    - chmod +x gocov-linux-amd64
    - ./gocov-linux-amd64 upload coverage.out
```

with `GOCOV_TOKEN` (masked, but **not protected** — GitLab checks
"Protect variable" by default, and protected variables never reach
merge request pipelines) and, when self-hosting, `GOCOV_SERVER` set
as CI/CD variables under **Settings → CI/CD → Variables** on the group
or project. The `workflow` rules run the job on merge requests and the
default branch without duplicate pipelines. Note that gitlab.com's free
tier requires a verified account (credit card) before shared runners
pick up jobs.

On runners without a Go toolchain, use the prebuilt binaries from
[GitHub Releases](https://github.com/gocov/gocov/releases) instead
(linux/darwin/windows, amd64 + arm64, checksums included). Pin a version
and cache the download on self-hosted runners:

```sh
ver=v0.1.0
arch=$(uname -m); case "$arch" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; esac
bin="$HOME/.cache/gocov/gocov-$ver-linux-$arch"
if [ ! -x "$bin" ]; then
  mkdir -p "$(dirname "$bin")"
  curl -fsSL "https://github.com/gocov/gocov/releases/download/$ver/gocov-linux-$arch" -o "$bin"
  chmod +x "$bin"
fi
"$bin" upload coverage.out
```
Outside CI, values fall back to git or can be passed explicitly:

```sh
gocov upload -server https://gocov.example -token $TOKEN \
  -repo myworkspace/myrepo -commit $(git rev-parse HEAD) -branch main \
  coverage.out
```

Other ecosystems upload their reports the same way — the format is
detected from the content, no flag needed:

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

JaCoCo paths are package-qualified (`com/example/Foo.java`); diff
coverage matches them against repo paths by suffix, so source roots like
`src/main/java` need no configuration.

### Multiple reports per commit (parts)

When a commit's coverage comes from several jobs — a backend suite, a
frontend suite, an e2e run — give each upload a `part` so gocov combines
them instead of letting the last one win:

```sh
gocov upload -part backend  coverage.out
gocov upload -part frontend coverage/lcov.info
gocov upload -part e2e      e2e-lcov.info
```

The part name can also come from `$GOCOV_PART`, which is handy for matrix
jobs that already expose the variant in the environment.

gocov keeps every upload but derives a **merged report** per commit from
the latest upload of each part, and drives the status, gate, PR comment,
Code Insights, badge and trend from that merged report. Re-uploading a
part (a CI retry) replaces it rather than double-counting. When two parts
report the same file, their line hit counts are summed, so a line covered
by any part counts as covered.

Part names are normalized (trimmed and lowercased) server-side, so
`Backend` and `backend` are the same part. Uploads without a `part` use the
reserved name `default`; passing `-part default` explicitly lands in that
same bucket, so single-job setups are unchanged — a one-part merged report
equals the upload.

Parts are merged as they arrive, in place. gocov does **not** wait for a
fixed set of parts: while the jobs are still uploading, the merged report
reflects only the parts received so far, so its total can read low and the
gate can fail until the last part lands, then correct itself. If a reviewer
merges inside that window they may see an interim gate — sequence the gate
check after all coverage jobs, or wait for the final status. A future
`expected_parts` setting will let a repo hold status until every part is in;
until then the self-healing behaviour above is the model.

## Badge

```markdown
![coverage](https://gocov.example/badge/myworkspace/myrepo.svg)
```

Red below 50%, yellow 50–75%, green above 75%. Shows the latest upload on
the repo's default branch.

## API

`POST /api/v1/upload` — multipart form, `Authorization: Bearer <token>`

| part      | meaning                                        |
|-----------|------------------------------------------------|
| `profile` | file: the coverage profile                     |
| `repo`    | optional; must match the token's repo          |
| `commit`  | required commit SHA                            |
| `branch`  | defaults to the repo's default branch          |
| `pr_id`   | optional pull request id                       |
| `format`  | `go`, `lcov`, `jacoco` or `cobertura`; omitted → detected from content |
| `path_prefix` | maps profile paths to repo paths for diff coverage, e.g. the Go module path (the CLI fills it from go.mod) |
| `part`    | optional; names one slice of the commit's coverage (`backend`, `frontend`, `e2e`, …) uploaded from a separate CI job. Normalized to a lowercase slug (`[a-z0-9._-]`, ≤64); omitted or blank → `default`. Re-uploading a part replaces it. |

Returns `201` with `{id, total_pct, covered_stmts, total_stmts,
delta_pct, build_status}`. Uploads carrying a `pr_id` additionally get
`diff_pct`, `diff_covered_lines`, `diff_total_lines`, `diff_status` and
`pr_comment` when the repo's workspace is connected to its forge.

## Configuration

| variable                       | default                 |                             |
|--------------------------------|-------------------------|-----------------------------|
| `DATABASE_URL`                 | —                       | Postgres DSN (required)     |
| `GOCOV_ADDR`                   | `:8080`                 | listen address              |
| `GOCOV_BASE_URL`               | `http://localhost:8080` | public URL used in statuses |
| `GOCOV_GITHUB_APP_ID`          | —                       | GitHub App id; with the key, enables one-click workspace connect |
| `GOCOV_GITHUB_APP_PRIVATE_KEY` | —                       | the App's private key: PEM content, or a path to the PEM file |
| `GOCOV_SECRET_KEY`             | —                       | at-rest encryption key (long random value, e.g. `openssl rand -hex 32`); with the Bitbucket OAuth consumer, enables one-click workspace connect |
| `GOCOV_OAUTH_BITBUCKET_KEY`    | —                       | Bitbucket OAuth consumer key; with the secret, turns on web UI sign-in |
| `GOCOV_OAUTH_BITBUCKET_SECRET` | —                       | Bitbucket OAuth consumer secret |
| `GOCOV_OAUTH_GITHUB_KEY`       | —                       | GitHub OAuth app client id; with the secret, turns on web UI sign-in |
| `GOCOV_OAUTH_GITHUB_SECRET`    | —                       | GitHub OAuth app client secret |
| `GOCOV_OAUTH_GITLAB_KEY`       | —                       | GitLab OAuth application id; with the secret, turns on web UI sign-in |
| `GOCOV_OAUTH_GITLAB_SECRET`    | —                       | GitLab OAuth application secret |
| `GOCOV_ALLOWED_WORKSPACES`     | derived from tracked repos | comma-separated workspace/org slugs allowed to sign in |
| `GOCOV_MODE`                   | `private`               | `hosted` opens sign-in to any forge account with self-service workspace registration |

Forge access is per repo, through its workspace's one-click connection:
a GitHub App installation, or a Bitbucket/GitLab grant. A repo whose
workspace has no connection has no forge access — build statuses, PR
comments, diff coverage and default-branch detection are skipped for it,
while coverage is still stored and reported.

Forge access is always granted through the workspace's one-click
connection — there is no manual bot token to provision. The OAuth
consumer/app used for web UI sign-in is separate from the connection
grant and only needs the account/email read permissions described under
"Enable sign-in".

### Bitbucket connection

The Bitbucket connect grant covers every surface: build status, Code
Insights report and annotations, PR diff coverage, PR comments, source
view and default branch. Posts visibly appear as the account that
clicked Connect, so teams with a bot account should connect with it.
Because the grant carries that account's identity, gocov recognizes its
own earlier PR comment and updates it in place rather than stacking new
ones.

### GitHub App

The GitHub App covers every surface, check runs included — nothing to
provision per repo beyond installing the app on the org or account.
gocov recognizes its own PR comment by its `**gocov**` marker and
updates it in place.

To make the coverage gate blocking on GitHub, add a branch protection
rule under **Settings → Branches → Require status checks to pass** and
pick `gocov` (the commit status) or `gocov coverage` (the check run). A
failed gate then blocks the merge.

### GitLab connection (one-click)

GitLab workspaces are connected with one OAuth consent: on the workspace
settings (or setup) page, **Connect workspace** sends a member through
GitLab's consent screen with the `api` scope, and from then on statuses,
MR comments, diffs and source fetch act through that grant. Posts
visibly appear as the account that clicked Connect, so teams with a bot
account should connect with it. The grant's refresh token is stored
encrypted (GOCOV_SECRET_KEY) and rotates on every use; revoking the
application on GitLab (or the member leaving) skips the forge surfaces
and surfaces a "reconnect" prompt.

Requirements on top of GitLab sign-in: `GOCOV_SECRET_KEY` must be set,
and the GitLab OAuth application must carry the **`api` scope in
addition to** `read_user` and `read_api` — sign-in keeps requesting only
the read scopes; the bigger consent happens solely on Connect.

gocov recognizes its own MR note by its `**gocov**` marker and updates
it in place. To make the coverage gate blocking on GitLab, use
**Settings → Merge requests → Status checks** policies that reference
the `gocov` commit status; a failed gate then blocks the merge. GitLab
has no check-run equivalent — the MR note's diff coverage table is the
in-MR surface, and uploads report `code_insights: skipped` by design.

## Development

```sh
go test ./...
go build ./...
```

The store, forge and blobstore interfaces each have test doubles
(`internal/store/memory`, `internal/forge/fake`,
`internal/blobstore/memory`), so handlers are fully testable without
Postgres or a forge.

The Postgres store additionally has integration tests that run against a
real server when `GOCOV_TEST_DATABASE_URL` is set (they are skipped
otherwise). Each test creates and drops its own scratch database:

```sh
docker run --rm -d --name gocov-test-db -p 5433:5432 \
  -e POSTGRES_USER=gocov -e POSTGRES_PASSWORD=gocov -e POSTGRES_DB=gocov \
  postgres:18-alpine
GOCOV_TEST_DATABASE_URL=postgres://gocov:gocov@localhost:5433/gocov go test ./...
docker stop gocov-test-db
```

`GET /healthz` reports readiness (checks database connectivity) for load
balancers and container orchestrators; the server shuts down gracefully
on SIGINT/SIGTERM.

## License

AGPL-3.0 — see [LICENSE](LICENSE).
