# Multiple reports per commit (parts)

When a commit's coverage comes from several jobs — a backend suite, a frontend suite, an e2e run — give each upload a
`part` so gocov combines them instead of letting the last one win:

```sh
gocov upload -part backend  coverage.out
gocov upload -part frontend coverage/lcov.info
gocov upload -part e2e      e2e-lcov.info
```

The part name can also come from `$GOCOV_PART`, which is handy for matrix jobs that already expose the variant in the
environment.

gocov keeps every upload but derives a **merged report** per commit from the latest upload of each part, and drives the
status, gate, PR comment, Code Insights, badge and trend from that merged report. Re-uploading a part (a CI retry)
replaces it rather than double-counting. When two parts report the same file, their line hit counts are summed, so a
line covered by any part counts as covered.

Part names are normalized (trimmed and lowercased) server-side, so
`Backend` and `backend` are the same part. Uploads without a `part` use the reserved name `default`; passing
`-part default` explicitly lands in that same bucket, so single-job setups are unchanged — a one-part merged report
equals the upload.

Parts are merged as they arrive, in place. gocov does **not** wait for a fixed set of parts: while the jobs are still
uploading, the merged report reflects only the parts received so far, so its total can read low and the gate can fail
until the last part lands, then correct itself. If a reviewer merges inside that window they may see an interim gate —
sequence the gate check after all coverage jobs, or wait for the final status. A future
`expected_parts` setting will let a repo hold status until every part is in; until then the self-healing behaviour above
is the model.
