# Hosted deployment (app.gocov.dev)

Single EC2 instance (docker compose: gocov-server + Caddy) in front of an
RDS Postgres. TLS via Caddy/Let's Encrypt, DNS on Cloudflare (DNS-only).
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
cd /opt/gocov/deploy && docker compose -f docker-compose.prod.yml up -d --build
```

.env keys: DATABASE_URL (RDS), GOCOV_BASE_URL=https://app.gocov.dev,
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

Upgrades: `git -C /opt/gocov pull && cd /opt/gocov/deploy && docker compose -f docker-compose.prod.yml up -d --build`
(migrations apply automatically on start).
