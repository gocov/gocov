-- One-Click Connect P1 (GitHub App): a workspace may be linked to a
-- GitHub App installation instead of stored credentials. Only the
-- numeric installation id lands in the database — the app private key
-- stays in the server environment, and installation tokens are minted
-- on demand and never persisted.
ALTER TABLE workspaces
    ADD COLUMN github_installation_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN github_app_broken BOOLEAN NOT NULL DEFAULT FALSE;
