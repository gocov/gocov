-- Public report pages: visibility caches what the forge last reported
-- about the repo (refreshed on upload; '' until asked, treated as
-- private), and public_reports_disabled is the repo-settings switch that
-- turns anonymous report pages off for a public repo.
ALTER TABLE repos
    ADD COLUMN visibility TEXT NOT NULL DEFAULT '',
    ADD COLUMN public_reports_disabled BOOLEAN NOT NULL DEFAULT FALSE;
