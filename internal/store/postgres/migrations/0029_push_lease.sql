-- The forge push of a commit's merged report is claimed through a lease on
-- its row instead of an advisory lock held across the forge calls, so no
-- database connection waits on a forge (internal/store/postgres ClaimPush).
-- push_lease_until is when the current claim lapses (NULL: none);
-- push_dirty says an upload arrived while it was held, and the holder must
-- push again with the newest state.
ALTER TABLE commit_reports
    ADD COLUMN push_lease_until TIMESTAMPTZ,
    ADD COLUMN push_dirty       BOOLEAN NOT NULL DEFAULT false;
