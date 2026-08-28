# Forge apps & credentials

This page is for self-hosters. It sets up what makes the **Connect workspace** button work on your instance — what
users then do with that button is [Connect your forge](connecting.md); on the hosted service all of this is already
done.

Forge access is per workspace, through its one-click connection: a GitHub App installation, or a Bitbucket/GitLab
OAuth grant. There is no manual bot token to provision. The OAuth consumer/app used for
[web UI sign-in](sign-in.md) is separate from the connection grant and only needs the account/email read permissions
described there.

## GitHub App

Register a GitHub App (**Settings → Developer settings → GitHub Apps → New GitHub App**) with:

- **Setup URL**: `https://your-gocov-host/github/setup`, with *Redirect on update* enabled
- **Repository permissions**: *Checks: Read & write*, *Commit statuses: Read & write*, *Pull requests: Read & write*,
  *Contents: Read-only*, *Metadata: Read-only*
- **Organization permissions**: *Members: Read-only* (org membership for sign-in sync)
- **Webhook**: optional. gocov's model is upload-driven, so a self-hosted app can leave it disabled — installs are
  linked through the setup redirect and uninstalls are detected lazily. A **Marketplace listing requires it**: set the
  webhook URL to `https://your-gocov-host/github/webhook`, a webhook secret, and `GOCOV_GITHUB_WEBHOOK_SECRET` to the
  same value on the server. The endpoint verifies each delivery's signature; it logs `marketplace_purchase` events and
  flips a workspace's app-broken flag on `installation` deleted/suspend/unsuspend

Generate a private key on the app page and set both variables on the server:

```sh
GOCOV_GITHUB_APP_ID=...
GOCOV_GITHUB_APP_PRIVATE_KEY=/path/to/gocov.private-key.pem  # or the PEM content itself
```

Members then connect from the workspace settings or setup page; after GitHub's install screen they land back on gocov
with the workspace connected. The App covers every surface, check runs included — it is the first-class Checks API
citizen, so check runs are not permission-fragile — and posts as the app's bot identity (e.g. `gocov[bot]`). In hosted
mode an install on an account with no workspace yet registers it on the spot (same claim rules as **/register**).

Handling and protecting the private key file is covered in [Self-hosting](self-hosting.md#the-github-app-private-key).

## Bitbucket

The Bitbucket connection rides the same OAuth consumer as [sign-in](sign-in.md), with two additions:

1. Extend the consumer's permissions beyond sign-in: **Account: Read**, **Email**, **Repositories: Write**,
   **Pull requests: Write**. (Bitbucket scopes live on the consumer, not the consent request, so the sign-in consent
   lists them too — sign-in itself still stores no forge tokens.)
2. Set an encryption key on the server:

```sh
GOCOV_SECRET_KEY=...   # exactly 64 hex characters: openssl rand -hex 32
```

The value is hex-decoded straight into the AES-256 key — there is no key-stretching step — so generate it with
`openssl rand -hex 32` rather than inventing a passphrase; the server refuses to boot on anything else.

The grant covers every surface: build status, Code Insights report and annotations, PR diff coverage, PR comments,
source view and default branch. Bitbucket has no app identity, so posts appear as the account that clicked Connect —
the UI states this at connect time, and teams with a bot account should connect with it.

## GitLab

Same shape as Bitbucket: the sign-in OAuth application, plus `GOCOV_SECRET_KEY`, plus one extra scope — the
application must carry **`api` in addition to** `read_user` and `read_api`. Sign-in keeps requesting only the read
scopes; the bigger consent happens solely on Connect. Posts appear as the account that clicked Connect.

## How grants are stored

The grant's refresh token is stored on the workspace, AES-256-GCM-encrypted under `GOCOV_SECRET_KEY`; access tokens
live only in memory. Bitbucket and GitLab rotate refresh tokens on every use — gocov persists each rotation
atomically. GitHub App connections do not use the secret key at all; they ride the App's private key.

If a grant dies — the connecting account leaves the workspace, the consent is revoked, or a Bitbucket token ages out
after three unused months — the next upload degrades to `skipped`, never to a failure, and the settings page offers a
reconnect. The same applies to a lost or rotated `GOCOV_SECRET_KEY`: nothing is lost but the grants, and the cost is
one reconnect per Bitbucket/GitLab workspace ([Self-hosting](self-hosting.md#the-secret-key) has the backup advice).
