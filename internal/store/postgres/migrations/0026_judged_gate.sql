-- The gate an upload and a commit report were judged against, as JSON
-- ({"min_coverage", "min_diff_coverage", "max_coverage_drop"}, null for a
-- rule that was off). The verdict, its explanation and the dashboard read
-- it instead of the repo's gate as it is today, so editing the gate later
-- no longer rewrites history ("failed" beside "above the minimum", or a
-- failed commit reading as ungated). NULL on rows judged before it was
-- recorded; those fall back to the current gate.
ALTER TABLE uploads ADD COLUMN gate jsonb;
ALTER TABLE commit_reports ADD COLUMN gate jsonb;
