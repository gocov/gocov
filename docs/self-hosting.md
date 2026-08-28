# Self-hosting in production

[Getting started](getting-started.md) takes you from `docker compose up` to a first upload on your own machine. This
page is the difference between that and an instance other people upload to: TLS in front, a database you did not start
with the app, a key you can lose, and upgrades that have to be boring. The process itself stays small — almost
everything here is about what surrounds it.

## What you are running

One process and one Postgres. Everything gocov persists lives in Postgres — coverage history, workspaces, repos, gates,
sessions, and the raw uploaded profiles themselves, because the blob store is a Postgres table too. The container writes
nothing to disk, so it can be rebuilt or replaced without moving state, and there is exactly one thing to back up.

The image is distroless: a static binary, CA certificates and nothing else — no shell, no package manager — running as
uid **65532**. There is no published container image yet, so both compose files build it from the repo. If you would
rather run the binary under systemd, every release ships `gocov-server` for linux, darwin and windows on amd64 and
arm64, with `checksums.txt` alongside.

All of it is AGPL-3.0, and on an instance you run yourself the license does more work than it does on a page you merely
visit. The server contacts nothing but your database and the forge APIs — no telemetry, no license check, no call home
— so a running deployment depends on this project's servers not at all, and on this project only for the next version
you choose to build. That is the part of "self-hostable" that survives a change of ownership, funding or interest.

The footprint is modest. gocov's own hosted instance runs the server and a TLS terminator on a single 2 vCPU / 2 GB
arm64 VM, in front of a 2 vCPU / 1 GB managed Postgres.

The repo ships a starting point for this shape under `deploy/`: `docker-compose.prod.yml` runs the server behind a Caddy
TLS terminator and expects Postgres to be external, and the `Caddyfile` next to it is the one quoted below. The root
`docker-compose.yml` is the evaluation stack and is not the same thing — it brings its own Postgres.

## TLS and the reverse proxy

The server speaks plain HTTP on `GOCOV_ADDR` (`:8080` by default), so terminate TLS in front of it.

Set `GOCOV_BASE_URL` to the public `https://` URL. This is not cosmetic: gocov builds every outgoing link from it —
build statuses, PR comments, badges — and auth cookies are marked `Secure` exactly when it starts with `https://`. An
instance served over TLS whose base URL still says `http://` hands out session cookies without the `Secure` attribute.

Because outgoing URLs come from that one variable, gocov never has to trust `X-Forwarded-Proto` or `X-Forwarded-Host`.
There is no proxy-header configuration to get wrong.

One proxy setting does matter: the request body limit. The server caps an upload at **64 MiB**; a proxy with a smaller
limit (nginx defaults to 1 MB) rejects large reports before gocov ever sees them, and the CLI surfaces the proxy's error
instead of a gocov one. Allow at least 64 MiB — `client_max_body_size 64m` on nginx, or with Caddy:

```caddyfile
gocov.example.com {
	request_body {
		max_size 100MB
	}
	reverse_proxy server:8080
	encode gzip

	header {
		Strict-Transport-Security "max-age=31536000"
		X-Content-Type-Options "nosniff"
	}
}
```

## Postgres

Bring your own: the DSN goes in `DATABASE_URL`, and connection options such as `sslmode=require` go in the URL. The
Postgres in `docker-compose.yml` is for evaluation only — fixed development credentials, a container volume, the same
host as the app.

Migrations are embedded in the binary and applied at start-up, in filename order, tracked in a `schema_migrations`
table. A deploy is therefore "new image, restart", never a separate migrate step — but the database user does need DDL
rights on its schema, not just read/write.

They are forward-only: there are no down migrations. Rolling the binary back does not roll the schema back, so crossing
a schema change in reverse means restoring from a backup.

## The secret key

`GOCOV_SECRET_KEY` is exactly 64 hex characters — `openssl rand -hex 32`. It is raw AES-256 key material, not a
passphrase: a memorable string is refused at boot rather than stretched by a KDF, and a malformed value stops the
process instead of degrading a feature.

It encrypts Bitbucket and GitLab grant refresh tokens at rest (AES-256-GCM). GitHub App connections do not use it —
those ride the App's private key.

Losing or rotating the key does not lose coverage data. A refresh token that no longer decrypts comes back marked
broken, the workspace shows as needing a reconnect, and reconnecting re-seals it under the current key. The cost of
rotation is one reconnect per Bitbucket/GitLab workspace; there is no re-encryption tool and none is needed.

Back the key up, and not only inside the database dump. A dump restored without its key is a dump every connected
workspace has to reconnect by hand.

## The GitHub App private key

`GOCOV_GITHUB_APP_PRIVATE_KEY` takes either PEM content or a path to the PEM file. In a container the path plus a
read-only mount is easier to live with than a multi-line environment variable.

The container runs as uid 65532 and the server exits when it cannot read a key it was configured with, so wrong
ownership is a crash loop rather than a missing feature. Keep the file `600` and give it to that uid instead of opening
it up — the key can mint installation tokens for every repo the App is installed on:

```sh
sudo chown 65532 github-app.pem
```

Check it without touching the running service:

```sh
docker run --rm --user 65532:65532 -v "$PWD/github-app.pem:/run/k.pem:ro" alpine:3.21 cat /run/k.pem > /dev/null && echo readable
```

## Upgrading

Fetch the new code, rebuild or replace the binary, restart; migrations apply themselves on the way up. On the compose
deployment above that is:

```sh
git pull && docker compose -f docker-compose.prod.yml up -d --build
```

Detached, because this is the long-running instance — unlike the foreground `docker compose up` the quick start uses to
show you the boot log.

A restart is the one place the shutdown budget shows. `SIGTERM` drains in-flight requests for up to 15 seconds, which
is longer than Docker's 10-second default stop timeout — raise `stop_grace_period` above it, or a rolling restart will
cut an upload in half. Kubernetes' 30-second default already clears it.

`gocov-server version` reports what is actually running — the version is stamped at build time, derived from git when
the image is built.

The server and the upload CLI version independently, so pin the CLI in CI on its own schedule; see
[Uploading coverage](ci-upload.md).

## What talks to what

| direction    | traffic                                                                     |
|--------------|-----------------------------------------------------------------------------|
| inbound 443  | browsers, CI uploads, and GitHub App webhook deliveries if you enabled them |
| outbound 443 | forge APIs — statuses, PR comments, check runs, diffs                       |
| Postgres     | from the server only; it never needs to be reachable from anywhere else     |

Every variable named on this page, and the rest of them, are listed in [Configuration](configuration.md). Setting up the
forge side is [Forge connections](forge-connections.md); who may sign in is [Sign-in](sign-in.md).
