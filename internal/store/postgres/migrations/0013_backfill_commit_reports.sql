-- Backfill merged reports from existing uploads so badge, dashboard, trend,
-- delta and the gate's drop baseline keep working the moment this deploys,
-- instead of reading an empty commit_reports until every repo re-uploads.
--
-- Kept as its own migration (not folded into 0012) so a database that already
-- applied an earlier revision of 0012 still runs the backfill; and written
-- ON CONFLICT DO NOTHING so it is safe to run by hand and never disturbs a
-- report the live recompute has since written.
--
-- Pre-feature history is one upload per commit, which is exactly a one-part
-- merged report; take the latest upload per (repo, commit) — 0011 stamped
-- legacy rows part='default'. upload_id/status_pushed_version seed from that
-- upload's id, and ordering the insert by it makes commit_reports.id ascend
-- with commit order, which every read's "ORDER BY id DESC" assumes.
INSERT INTO commit_reports (repo_id, commit_sha, branch, pr_id, total_pct,
    covered_stmts, total_stmts, gate_failed, diff_coverage, part_count,
    upload_id, status_pushed_version, created_at, updated_at)
SELECT repo_id, commit_sha, branch, pr_id, total_pct, covered_stmts, total_stmts,
       gate_failed, diff_coverage, 1, id, id, created_at, created_at
FROM (
    SELECT DISTINCT ON (repo_id, commit_sha) *
    FROM uploads
    ORDER BY repo_id, commit_sha, id DESC
) latest
ORDER BY latest.id
ON CONFLICT (repo_id, commit_sha) DO NOTHING;
