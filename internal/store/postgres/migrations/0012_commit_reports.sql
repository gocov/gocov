-- A commit_report is the merged coverage of a commit, derived from the
-- latest upload of each part and recomputed on every upload. It is what
-- status, gate, PR comment, insights, badge and trend read, so a commit
-- whose coverage arrives in several parts shows its combined total instead
-- of whichever part uploaded last. One upload per commit degenerates to a
-- single-part merged report, identical to the upload.
CREATE TABLE commit_reports (
    id            BIGSERIAL        PRIMARY KEY,
    repo_id       BIGINT           NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    commit_sha    TEXT             NOT NULL,
    branch        TEXT             NOT NULL,
    pr_id         TEXT             NOT NULL DEFAULT '',
    total_pct     DOUBLE PRECISION NOT NULL,
    covered_stmts BIGINT           NOT NULL,
    total_stmts   BIGINT           NOT NULL,
    gate_failed   BOOLEAN          NOT NULL DEFAULT false,
    diff_coverage JSONB,
    part_count    INTEGER          NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ      NOT NULL DEFAULT now(),
    UNIQUE (repo_id, commit_sha)
);

-- Serves "latest (passed) merged report on a branch" for the delta baseline
-- and, later, the badge and trend. id ascends with first-seen commit order,
-- which is the ordering those reads want.
CREATE INDEX commit_reports_repo_branch_idx ON commit_reports (repo_id, branch, id DESC);

-- Backfill from existing uploads so badge, dashboard, trend, delta and the
-- gate's drop baseline all keep working the moment this deploys, instead of
-- reading an empty table until every repo re-uploads. Pre-feature history is
-- one upload per commit, which is exactly a one-part merged report; take the
-- latest upload per (repo, commit) — 0011 already stamped legacy rows
-- part='default'. Ordering the insert by the source upload id makes
-- commit_reports.id ascend with commit order, which every read's
-- "ORDER BY id DESC" assumes.
INSERT INTO commit_reports (repo_id, commit_sha, branch, pr_id, total_pct,
    covered_stmts, total_stmts, gate_failed, diff_coverage, part_count, created_at, updated_at)
SELECT repo_id, commit_sha, branch, pr_id, total_pct, covered_stmts, total_stmts,
       gate_failed, diff_coverage, 1, created_at, created_at
FROM (
    SELECT DISTINCT ON (repo_id, commit_sha) *
    FROM uploads
    ORDER BY repo_id, commit_sha, id DESC
) latest
ORDER BY latest.id;
