# Public report pages

For a repository its forge reports as **public**, gocov serves the report pages read-only without sign-in: the repo
overview (`/repos/{workspace}/{repo}`), each upload's report, the per-file source view and the raw profile download.
Anyone who lands there from a badge, a PR comment's *Full report* link or a build status sees the same coverage the
world can already read the code of — settings, tokens and every mutating action stay members-only.

## What makes a repo public

Two things, both of which you control:

- **The forge says so.** gocov asks the forge for the repo's visibility and caches the answer, refreshing it on every
  upload — so a repo flipped private on the forge closes its pages by the next upload at the latest, and a repo flipped
  public opens them the same way. A repo whose workspace has no [forge connection](connecting.md) counts as private:
  gocov never guesses.
- **The switch is on.** Repo settings grow a **Public reports** switch for public repos, on by default. Turning it off
  closes the pages to members only, immediately. Private repos don't show the switch and are never served publicly.

Anonymous visitors get exactly what a signed-out browser always got on everything else: private repos — and slugs that
don't exist — answer with the sign-in redirect, indistinguishably.

## What shows on a public page

The same report members see, minus the member chrome: no settings, no tokens, no actions. Uploads that arrived
[tokenless from a fork PR](pull-requests.md#fork-prs-without-a-token) keep their "unverified contributor upload"
marker on the public page too.

## The badge links here

The badge snippet the repo page and repo settings hand out links the SVG to the repo's report page, so a README
reader can click through from the number to the report behind it:

```markdown
[![coverage](https://app.gocov.dev/badge/myworkspace/myrepo.svg)](https://app.gocov.dev/repos/myworkspace/myrepo?ref=badge)
```

Self-hosted instances serve the same pages from their own host, and the operator can turn public report pages off
instance-wide — see [sign-in & access](sign-in.md).
