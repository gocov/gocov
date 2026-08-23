# Getting started

## Quick start

```sh
docker compose up
```

This starts Postgres and the server on http://localhost:8080 (migrations apply automatically).

## Onboarding (in the web UI)

Workspaces, repos, tokens, gates and forge connections are all administered from the web UI — there is no server-side
admin CLI. Because onboarding derives the workspaces you may register from your signed-in forge identity, the first step
is to **enable sign-in** (see
[Sign-in](sign-in.md)); a fully open instance with no provider configured has no identity to register from.

Once a sign-in provider is set:

1. Sign in with your forge account.
2. The onboarding wizard registers your workspace (the org/group/user the forge lists for you) and shows its **upload
   token — once**.
3. Set that token as a *workspace variable* (`GOCOV_TOKEN`, secured)
   together with `GOCOV_SERVER`; every repo under the workspace inherits them. Repos register themselves on their first
   upload — their default branch is asked from the forge when the workspace is connected, falling back to the
   workspace's default.
4. Connect the workspace to its forge (GitHub App, or a Bitbucket/GitLab one-click grant) so gocov can post statuses,
   PR/MR comments and check runs — see [Forge connections](forge-connections.md). A workspace with no connection still
   stores and reports coverage; the forge surfaces are simply skipped.

A workspace can sit at any level of a namespace tree: a GitHub org, a GitLab group or subgroup (registered by its full
path), a Bitbucket workspace, or a personal namespace — repos below it register themselves on first upload.

After onboarding, manage everything from the dashboard: each workspace links to its **settings** page (rotate the upload
token, set the default branch and coverage-gate defaults, connect/disconnect the forge). See
"Workspace settings in the UI" below.

## Hosted mode (self-service signup)

`GOCOV_MODE=hosted` turns the instance into a self-service one: any forge account may sign in, and a user who belongs to
no tracked workspace lands on **/register**, which lists the workspaces their forge account is a member of (captured at
sign-in). Claiming one creates the workspace, makes the user a member and shows the upload token — once; afterwards it
can only be rotated. Only workspaces the forge itself reports for the account can be registered, so there is nothing to
dispute: if a colleague registered your workspace first, signing in simply makes you a member.

Registration lands on an onboarding page: the forge-appropriate CI snippet with the server URL and token pre-filled, and
a live "waiting for your first upload" state that flips to the repo link once coverage arrives.

The default (`GOCOV_MODE=private`) keeps exactly the behavior described in these docs — self-hosted deployments upgrade
with zero change. Hosted mode requires at least one sign-in provider.

## Workspace settings in the UI

Signed-in members manage their workspaces from the dashboard (private and hosted mode alike): rotate the upload token
(the old one dies immediately; the new one is shown once), change the default branch and gate defaults for
auto-registered repos, and connect the workspace to its forge (GitHub App, or a Bitbucket/GitLab one-click grant) for
statuses, PR comments and insights. The upload token is never rendered back — it is shown once, right after a rotation.
This is the only way workspaces are administered; there is no server-side admin CLI.

There is no user bookkeeping to manage: accounts are provisioned on first sign-in and access is re-derived from forge
membership at each login (not per request), so removing someone from the workspace on the forge removes their access at
their next login. Sessions last 30 days.

Access mirrors your forge workspace membership. Once sign-in is configured, each account sees only the repos in the
workspaces and orgs the forge says it belongs to — the repo list is filtered, and a direct link to another workspace's
repo, upload or source page returns 404. Memberships are synced from the forge on every sign-in, so there is no separate
invite or member-management step: add someone to the workspace on the forge and they see its coverage at their next
login; remove them and it disappears. A single-team self-host where everyone belongs to the same workspace is
unaffected, as is an instance with sign-in left open — both stay exactly as before.

## Next steps

- [Uploading from CI](ci-upload.md) — wire your pipelines up
- [Coverage gate](coverage-gate.md) — set minimums and make them block merges
- [Configuration](configuration.md) — the full environment variable reference
