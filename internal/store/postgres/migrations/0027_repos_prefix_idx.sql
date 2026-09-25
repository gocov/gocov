-- The repos under a workspace prefix (ListWorkspaceRepos: the dashboard,
-- once per membership, and the workspace pages). A LIKE 'prefix/%' cannot
-- use repos' (forge, slug) index under a non-C collation, nor under any
-- collation once the pattern is a bound parameter, so every such read
-- scanned the whole table. The reads now ask for a byte-wise range,
-- slug >= 'prefix/' AND slug < 'prefix0' in the "C" collation, which this
-- index serves.
CREATE INDEX repos_forge_slug_c_idx ON repos (forge, slug COLLATE "C");
