-- The forge push of a commit's merged report is claimed through a lease on
-- its row instead of an advisory lock held across the forge calls, so no
-- database connection waits on a forge (internal/store/postgres ClaimPush).
-- push_lease_until is when the current claim lapses (NULL: none). Work the
-- forge has not seen needs no column of its own: it is an upload of the
-- commit newer than status_pushed_version.
ALTER TABLE commit_reports ADD COLUMN push_lease_until TIMESTAMPTZ;
