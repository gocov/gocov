-- Per-repo ignore patterns: report paths matching any of them are dropped
-- from every upload before totals, diff coverage, the gate and the merge
-- see them (internal/ignore). Applied to uploads received from then on.
ALTER TABLE repos ADD COLUMN ignore_paths text[] NOT NULL DEFAULT '{}';
