-- Drop stored forge credentials. Manually entered per-repo and
-- per-workspace bot credentials are removed as a feature: the only
-- remaining path to a forge is a one-click connection (GitHub App
-- installation, Bitbucket grant or GitLab grant). Repos and workspaces
-- with neither simply have no forge access — statuses, PR comments, diff
-- coverage and default-branch detection are skipped for them.
ALTER TABLE repos DROP COLUMN forge_credentials;
ALTER TABLE workspaces DROP COLUMN forge_credentials;
