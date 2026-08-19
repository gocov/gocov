-- Report retention (workspace settings v2 — Defaults card): how long
-- coverage uploads are kept before pruning. Stored as a number of days;
-- 0 means "keep forever". This is persisted from the settings UI now;
-- the pruning job that acts on it lands separately, so the column is a
-- recorded preference until then.
ALTER TABLE workspaces
    ADD COLUMN report_retention_days INTEGER NOT NULL DEFAULT 0;
