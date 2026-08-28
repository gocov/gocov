# Why your coverage changed

Most surprises come from one of a handful of mechanics rather than from the code. This page is the short list, in the
order they actually bite.

## What the percentage counts

Coverage is **statements**, not lines and not files: every statement in every file of the merged report, counted once,
covered over total. A file with 300 statements moves the number thirty times as far as one with 10, which is why a
well-tested small package can land and the total barely twitches. A report with no statements at all reads 0%, not
100%.

## What it is compared against

The delta beside the percentage — and the `Compared to` line on the repo page — is not the previous commit. It is the
**latest gate-passing merged report on the same branch**, ignoring the commit's own report so an earlier part is never
its own baseline. A feature branch with no passing history of its own falls back to the default branch.

Gate-failing uploads are recorded but never become a baseline. That is deliberate: re-running CI cannot launder a
failure into the new reference point, and a PR cannot walk coverage down one tolerated step at a time. The gate's
**max drop** rule goes further and always measures against the default branch's latest passing report, whatever branch
the upload is on.

So a delta can move without your coverage moving — because the baseline it points at changed.

## A part did not run

When a commit's coverage arrives in [parts](parts.md), the merged report is rebuilt from the latest upload of **every
part that exists**. A job that did not run this time contributes nothing, and its statements simply are not in the
merged report; the total drops even though no code changed. There is no carry-forward of a previous run's part.

This is the most common unexplained drop in a matrix build: a skipped job, a cancelled shard, a job that failed before
its upload step. Check the upload page for the commit — it names the part it carried and how many parts the report was
merged from.

## A part is still in flight

Every upload recomputes the whole commit, so between jobs the merged report is genuinely incomplete: the total reads
low and the gate can fail, then correct itself when the last part lands. The report heals; a reviewer who looked during
the window did not see a bug. Sequence the gate check after all coverage jobs rather than racing them.

## Diff coverage is empty while total coverage looks fine

Diff coverage matches the paths in your profile against the paths in the pull request's diff. With `-path-prefix` the
match is exact; without one gocov tries a suffix heuristic in both directions, which is what lets JaCoCo's
package-qualified paths (`com/example/Foo.java`) meet a repo path like `src/main/java/com/example/Foo.java`.

When neither matches — a profile recorded under a Go module path, a CI checkout directory baked into the paths, a
monorepo subdirectory — total coverage is still correct and diff coverage comes out empty, because gocov cannot tell
which recorded lines are the changed ones. Set `-path-prefix` to the prefix your profile carries.

## There is nothing to diff against

Diff coverage needs the pull request's diff, which gocov fetches from the forge. On a workspace with no
[forge connection](forge-connections.md), on a push that is not part of a pull request, or on an upload whose PR id was
not detected, there is no diff — diff coverage is skipped and the gate's diff rule is skipped with it. Total coverage
and the gate's other rules are unaffected.
