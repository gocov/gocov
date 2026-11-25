-- A commit's merged report used to follow the newest upload of the commit,
-- so a commit the default branch had built moved off it as soon as another
-- branch uploaded the same commit — a branch cut from it, a fast-forward, a
-- PR whose head it is — leaving a gap in the default branch's trend and,
-- for its newest commit, a blank dashboard row and badge until the default
-- branch uploaded the commit again. The recompute now keeps such a report
-- on the default branch (store.Store.UpsertCommitReport); this moves back
-- the reports that left before it did: every report not on its repo's
-- default branch as a build of the branch itself, whose commit has an
-- upload on that branch with no PR.
--
-- updated_at stays: it is when the commit was last uploaded (the
-- dashboard's staleness), and nothing was. Running it again moves nothing.
UPDATE commit_reports cr
SET branch = r.default_branch, pr_id = ''
FROM repos r
WHERE r.id = cr.repo_id
  AND NOT (cr.branch = r.default_branch AND cr.pr_id = '')
  AND EXISTS (
      SELECT 1 FROM uploads u
      WHERE u.repo_id = cr.repo_id AND u.commit_sha = cr.commit_sha
        AND u.branch = r.default_branch AND u.pr_id = ''
  );
