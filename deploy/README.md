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
GOCOV_GITHUB_APP_PRIVATE_KEY (PEM content), GOCOV_OAUTH_GITHUB_KEY/SECRET,
GOCOV_OAUTH_BITBUCKET_KEY/SECRET.

Upgrades: `git -C /opt/gocov pull && cd /opt/gocov/deploy && docker compose -f docker-compose.prod.yml up -d --build`
(migrations apply automatically on start).
