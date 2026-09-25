-- The default-branch total the coverage gate's drop rule compared against
-- when it judged the row, so the page explaining the verdict narrates the
-- comparison the gate actually made instead of recomputing one against a
-- default branch that has moved on since. NULL when the drop rule was off
-- or had nothing to compare against, and on rows judged before this was
-- recorded.
ALTER TABLE uploads ADD COLUMN gate_base_pct double precision;
ALTER TABLE commit_reports ADD COLUMN gate_base_pct double precision;
