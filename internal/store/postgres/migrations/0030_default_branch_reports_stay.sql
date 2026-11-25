-- A commit's merged report used to follow its newest upload, so a commit
-- the default branch had built left that branch's trend, dashboard row and
-- badge as soon as another branch or PR uploaded the same commit. The
-- recompute now keeps it there (core.Pipeline.Recompute); this moves back
-- the reports that left before it did: every report whose commit has an
-- upload on its repo's default branch with no PR. Their numbers stay the
-- last upload's until the commit is uploaded again.
--
-- updated_at stays: it is when the commit was last uploaded (the
-- dashboard's staleness), and nothing was. Running it again moves nothing.
UPDATE commit_reports cr
SET branch = r.default_branch, pr_id = ''
FROM repos r
WHERE r.id = cr.repo_id
  AND (cr.branch, cr.pr_id) <> (r.default_branch, '')
  AND EXISTS (
      SELECT 1 FROM uploads u
      WHERE u.repo_id = cr.repo_id AND u.commit_sha = cr.commit_sha
        AND u.branch = r.default_branch AND u.pr_id = ''
  );
