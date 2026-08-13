-- A part names one slice of a commit's coverage (e.g. "backend", "frontend",
-- "e2e") uploaded from a separate CI job. Uploads stay immutable and
-- append-only; the merged report reads the latest upload per (commit, part),
-- so re-uploading the same part replaces it rather than double-counting.
-- Existing single-upload setups fall to the "default" part unchanged.
ALTER TABLE uploads ADD COLUMN part TEXT NOT NULL DEFAULT 'default';

-- Serves "latest upload per part for a commit" when the merged report is
-- recomputed on each upload.
CREATE INDEX uploads_repo_commit_part_idx ON uploads (repo_id, commit_sha, part, id DESC);
