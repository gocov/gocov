-- Carryforward: the parts a repo names here are taken, for a commit that
-- did not upload them, from the branch's earlier reports
-- (core.Pipeline.Recompute), and each report records the uploads it took
-- them from. Neither column changes a report already stored: reports are
-- computed with them from the next upload on.
ALTER TABLE repos ADD COLUMN carryforward_parts text[] NOT NULL DEFAULT '{}';
ALTER TABLE commit_reports ADD COLUMN carried_upload_ids bigint[] NOT NULL DEFAULT '{}';
