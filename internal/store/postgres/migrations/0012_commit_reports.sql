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
