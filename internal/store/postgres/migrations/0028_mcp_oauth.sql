-- Signing an MCP connector in (internal/server/mcpauth.go): the OAuth
-- authorization codes a consent issues, each good for one exchange, and the
-- access tokens they are exchanged for. Both keep only a hash, as sessions
-- do, and both go with their user. An access token reads reports and
-- nothing else, so it is a table of its own rather than a session: a
-- session cookie may change settings, a connector's token may not.

CREATE TABLE oauth_codes (
    code_hash      TEXT        PRIMARY KEY,
    user_id        BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    client_id      TEXT        NOT NULL,
    redirect_uri   TEXT        NOT NULL,
    code_challenge TEXT        NOT NULL,
    expires_at     TIMESTAMPTZ NOT NULL
);

CREATE TABLE mcp_tokens (
    token_hash  TEXT        PRIMARY KEY,
    user_id     BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    client_name TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);
