-- The visibility cache gets an age: visibility_checked_at is when the
-- forge last answered the visibility question (stamped by
-- SetRepoVisibility; NULL = never asked). The upload path skips re-asking
-- while the answer is fresh, and the anonymous serving path re-verifies a
-- stale answer in the background.
ALTER TABLE repos
    ADD COLUMN visibility_checked_at TIMESTAMPTZ;
