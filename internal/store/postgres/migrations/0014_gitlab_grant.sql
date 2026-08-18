-- GitLab Connect (one-click grant): a workspace may act through the
-- OAuth grant of the member who clicked Connect, mirroring the
-- Bitbucket grant columns. The refresh token is stored AES-GCM-encrypted
-- under GOCOV_SECRET_KEY (sealed by the application; the column only
-- ever sees "v1:<base64>" values). The account username is display
-- metadata: notes post as it.
ALTER TABLE workspaces
    ADD COLUMN gitlab_grant_account TEXT NOT NULL DEFAULT '',
    ADD COLUMN gitlab_refresh_token TEXT NOT NULL DEFAULT '',
    ADD COLUMN gitlab_grant_broken BOOLEAN NOT NULL DEFAULT FALSE;
