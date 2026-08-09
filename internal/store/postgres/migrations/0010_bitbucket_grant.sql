-- One-Click Connect P2 (Bitbucket grant): a workspace may act through
-- the OAuth grant of the member who clicked Connect. The refresh token
-- is stored AES-GCM-encrypted under GOCOV_SECRET_KEY (sealed by the
-- application; the column only ever sees "v1:<base64>" values). The
-- account username is display metadata: comments post as it (D8).
ALTER TABLE workspaces
    ADD COLUMN bitbucket_grant_account TEXT NOT NULL DEFAULT '',
    ADD COLUMN bitbucket_refresh_token TEXT NOT NULL DEFAULT '',
    ADD COLUMN bitbucket_grant_broken BOOLEAN NOT NULL DEFAULT FALSE;
