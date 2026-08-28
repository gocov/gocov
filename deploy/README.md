# Hosted deployment (app.gocov.dev)

Single EC2 instance (docker compose: gocov-server + Caddy) in front of an
RDS Postgres. Caddy serves a Cloudflare Origin CA certificate from
`deploy/certs/` (no ACME), behind Cloudflare's proxy on Full (strict).
Management via SSM Session Manager — the instance has no SSH port open.

Infra (eu-central-1):
- Security groups: `gocov-web` (80/443 open), `gocov-db` (5432 from gocov-web)
- IAM role + instance profile `gocov-ec2` (AmazonSSMManagedInstanceCore)
- RDS `gocov-db`: db.t4g.micro, Postgres 18, 20GB gp3, encrypted,
  7-day backups, not public
- EC2 `gocov-server`: t4g.small, AL2023 arm64, 30GB gp3 + Elastic IP

Server layout: repo cloned at `/opt/gocov`, secrets in `/opt/gocov/deploy/.env`
(never committed, `chmod 600`), run with:

```sh
cd /opt/gocov/deploy && docker compose -f docker-compose.prod.yml up -d
```

The compose file pulls `ghcr.io/gocov/gocov-server` at the exact version
pinned by `GOCOV_VERSION` in `.env` — nothing is built on this machine.

.env keys: GOCOV_VERSION (the deployed release tag — written by the deploy
workflow, not by hand), DATABASE_URL (RDS), GOCOV_BASE_URL=https://app.gocov.dev,
GOCOV_MODE=hosted, GOCOV_SECRET_KEY, GOCOV_GITHUB_APP_ID,
GOCOV_OAUTH_GITHUB_KEY/SECRET, GOCOV_OAUTH_BITBUCKET_KEY/SECRET.

The GitHub App private key is the exception: it is not in .env. The compose
file defaults GOCOV_GITHUB_APP_PRIVATE_KEY to `/run/gocov-app-key.pem` and
mounts `/opt/gocov/deploy/github-app.pem` there, and the server reads the
file because the value holds no PEM content. The container runs as **uid
65532** (distroless nonroot), so that file must be readable by that uid —
keep it `chmod 600` and give it to the container's user rather than opening
it up, because the key can mint installation tokens for every repo the App
is installed on:

```sh
sudo chown 65532 /opt/gocov/deploy/github-app.pem
```

Getting this wrong is a boot failure, not a degraded feature: the server
exits when it cannot read a configured key, so the container crash-loops.
Check it without touching the running service:

```sh
docker run --rm --user 65532:65532 -v /opt/gocov/deploy/github-app.pem:/run/k.pem:ro alpine:3.21 cat /run/k.pem > /dev/null && echo readable
```

## Deploys

Every release deploys itself: `release.yml` builds the multi-arch image,
pushes it to GHCR, then calls `deploy.yml`, which waits on the
`production` environment for approval. Approving is the whole human part
of a deploy. The workflow then assumes the `gocov-deploy` OIDC role, runs
the roll script on the instance over SSM (pin `GOCOV_VERSION`, `pull`,
`up -d`, wait for the container healthcheck), and smoke-tests the result:
`/healthz` plus a real upload to `gocov/smoke` with the release's own CLI
binary. Migrations apply automatically on start, as always.

**Rollback**: dispatch `deploy.yml` by hand with any older release tag —
same job, no build, seconds. Migrations are forward-only, so rolling the
image back never rolls the schema back; if the schema itself is suspect,
that is RDS point-in-time recovery, not a redeploy.

**Emergency path**: SSM Session Manager access is unchanged. The manual
equivalent of a deploy is editing `GOCOV_VERSION` in `.env` and
`docker compose -f docker-compose.prod.yml pull server && docker compose -f docker-compose.prod.yml up -d server`.

The deploy checks the repo at `/opt/gocov` out at the release tag before
touching compose, so the box's checkout follows releases (detached), not
`main` — the compose file and Caddyfile a deploy uses are the released
ones. A local edit to a tracked file on the box makes the next deploy
fail loudly at that checkout, on purpose.

### One-time setup the workflow depends on

- **GHCR package public**: the first push of `ghcr.io/gocov/gocov-server`
  creates a private package — make it public (package settings) and link
  it to the repo. Production pulls and the Fargate plan both rely on
  credential-free pulls.
- **GitHub environment `production`** on gocov/gocov with a required
  reviewer. This is the deploy gate; without it every release would
  deploy unattended.
- **AWS OIDC**: an IAM OIDC provider for `token.actions.githubusercontent.com`
  (audience `sts.amazonaws.com`) and a role `gocov-deploy` in account
  773658094601, trusted only for
  `repo:gocov/gocov:environment:production`, with a policy allowing
  exactly `ssm:SendCommand` on instance `i-0ec3687170963f061` + the
  `AWS-RunShellScript` document, and `ssm:GetCommandInvocation`.
- **Smoke repo `gocov/smoke`**: a public repo with a trivial Go module
  and one test, tracked in the gocov workspace on app.gocov.dev, so the
  workspace's `GOCOV_TOKEN` secret (already used by ci.yml) accepts its
  uploads.
