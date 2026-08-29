-- Tokenless fork-PR uploads (Şerit A): one accepted upload per
-- (workflow run, attempt, part) triple per repo. The first verified
-- upload inserts its triple here; a replay of the same triple hits the
-- primary key and is refused. Rows are only removed when the upload
-- behind them failed to land (so the CI retry can claim again) or when
-- the repo goes away.
CREATE TABLE tokenless_claims (
    repo_id     BIGINT NOT NULL REFERENCES repos (id) ON DELETE CASCADE,
    run_id      BIGINT NOT NULL,
    run_attempt BIGINT NOT NULL,
    part        TEXT   NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_id, run_id, run_attempt, part)
);
