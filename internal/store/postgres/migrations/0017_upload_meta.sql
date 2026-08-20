-- Upload provenance (upload detail v2 — Upload card): optional metadata
-- captured at upload time and shown on the upload page. Who uploaded (CLI
-- version and kind), the CI run it came from, the commit subject and author,
-- the raw profile's filename and size, and the server's processing time.
-- All best-effort: a single JSONB bag keyed by the store.UploadMeta fields,
-- NULL for uploads made before it was recorded or through the raw API.
ALTER TABLE uploads
    ADD COLUMN meta JSONB;
