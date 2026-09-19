import { repoSettingsPath, workspaceSettingsPath } from "./api/queries";
import { routes } from "./urls";

// A GitLab group nests (grp/sub). Its slash is a slash in every URL: a prefix
// is the route's trailing splat, never one %2F segment for a proxy to decode.
test("workspace pages keep a nested prefix's slashes", () => {
  expect(routes.dashboard()).toBe("/");
  expect(routes.dashboard("github/acme")).toBe("/w/github/acme");
  expect(routes.dashboard("gitlab/grp/sub")).toBe("/w/gitlab/grp/sub");
  expect(routes.workspace("gitlab", "grp/sub")).toBe("/workspace-settings/gitlab/grp/sub");
  expect(routes.workspaceSetup("gitlab", "grp/sub")).toBe("/workspace-setup/gitlab/grp/sub");
});

test("the workspace endpoints take the verb before the prefix, like the repo ones", () => {
  expect(workspaceSettingsPath("gitlab", "grp/sub")).toBe("/workspace-settings/gitlab/grp/sub");
  expect(workspaceSettingsPath("gitlab", "grp/sub", "save")).toBe("/workspace-settings/save/gitlab/grp/sub");
  expect(workspaceSettingsPath("github", "acme", "reveal-token")).toBe("/workspace-settings/reveal-token/github/acme");
  expect(repoSettingsPath("gitlab", "grp/sub/proj", "save")).toBe("/repo-settings/save/gitlab/grp/sub/proj");
});
