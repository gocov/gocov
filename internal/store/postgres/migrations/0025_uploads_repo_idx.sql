-- A repo's uploads newest first, across branches: the repo page's history
-- with "All branches" selected (ListUploads). The existing indexes lead
-- with branch or commit after repo_id, so without this one every repo
-- page read and top-N sorted all of the repo's uploads.
CREATE INDEX uploads_repo_idx ON uploads (repo_id, id DESC);
