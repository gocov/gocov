-- Workspace roles: a membership is either an owner's or a member's seat.
-- Like membership itself, the role mirrors the forge — org admin on
-- GitHub, group Owner on GitLab, workspace administrator on Bitbucket —
-- and is re-synced on every sign-in. Nothing enforces it yet; the
-- settings pages start requiring it in the follow-up.
ALTER TABLE workspace_members
    ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'member'
        CHECK (role IN ('owner', 'member'));

-- Every member has had full rights until now, so existing seats are
-- grandfathered as owners rather than silently demoted. The next login
-- replaces each with the forge's answer, and no workspace is left
-- without an owner in between.
UPDATE workspace_members SET role = 'owner';

-- The owner subset of the forge workspace snapshot the registration page
-- renders from (same lifecycle as forge_workspaces: refreshed at login,
-- NULL until the first post-roles login).
ALTER TABLE users ADD COLUMN IF NOT EXISTS forge_owned_workspaces JSONB;
