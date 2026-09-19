import { connectOutcome } from "./connect";

const POLICY = "https://github.com/organizations/acme/settings/oauth_application_policy";
const REAUTH = "/oauth/github/start?next=%2Fgithub%2Fsetup%3Finstallation_id%3D42";

test("a requested install is neither an error nor a dead end", () => {
  const out = connectOutcome("install_requested");
  expect(out.tone).toBe("neutral");
  expect(out.title).toBe("Install requested");
  expect(out.message).toMatch(/owners/);
  expect(out.actions).toEqual([]);
});

test("landing on the setup URL without an installation points at the install button", () => {
  const out = connectOutcome("no_installation");
  expect(out.tone).toBe("warn");
  expect(out.message).toMatch(/Install the gocov app/);
  expect(out.actions).toEqual([]);
});

test("an unconfirmed install offers another run at the same installation", () => {
  const out = connectOutcome("install_unconfirmed", { installationId: "42" });
  expect(out.tone).toBe("bad");
  expect(out.actions).toEqual([{ label: "Try again", href: "/github/setup?installation_id=42" }]);
});

test("an unconfirmed install with no installation to retry says so and stops", () => {
  expect(connectOutcome("install_unconfirmed", {}).actions).toEqual([]);
});

test("a hidden organization gets both escape hatches, named", () => {
  const out = connectOutcome("not_your_workspace", { ws: "acme", installationId: "42" });
  expect(out.tone).toBe("bad");
  expect(out.title).toBe("Not your workspace");
  expect(out.message).toMatch(/gocov can see the app installed on acme/);
  expect(out.message).toMatch(/third-party/);
  expect(out.actions).toEqual([
    { label: "Allow gocov on acme", href: POLICY, external: true },
    { label: "Sign in again", href: REAUTH },
  ]);
});

test("a member who has since become an admin is told to sign in again", () => {
  const out = connectOutcome("owners_only", { ws: "acme", installationId: "42" });
  expect(out.tone).toBe("warn");
  expect(out.title).toBe("Owners only");
  expect(out.message).toMatch(/member of acme, not an admin/);
  expect(out.actions).toEqual([{ label: "Sign in again", href: REAUTH }]);
});

test("an unknown code still says what happened", () => {
  const out = connectOutcome("something_else", { ws: "acme", installationId: "42" });
  expect(out).toEqual({
    tone: "bad",
    title: "Install not completed",
    message: "The GitHub App install could not be completed.",
    actions: [],
  });
});

test("an empty code is an unknown code", () => {
  expect(connectOutcome("").title).toBe("Install not completed");
});

// Everything below arrives in the query string, so none of it is trusted.

test.each([
  ["javascript:alert(1)"],
  ["acme/../../evil"],
  ["acme?x=1"],
  ["acme.evil.com"],
  ['"><img src=x>'],
  ["a".repeat(40)],
  [""],
])("an implausible account %j names no organization and builds no link", (ws) => {
  const out = connectOutcome("not_your_workspace", { ws, installationId: "42" });
  expect(out.message).toContain("that organization");
  if (ws !== "") expect(out.message).not.toContain(ws);
  expect(out.actions).toEqual([{ label: "Sign in again", href: REAUTH }]);
});

test.each([
  ["1 OR 1"],
  ["42abc"],
  ["../../evil"],
  ["javascript:alert(1)"],
  ["-1"],
  ["9".repeat(20)],
  [""],
])("an implausible installation id %j builds no sign-in link", (id) => {
  const out = connectOutcome("owners_only", { ws: "acme", installationId: id });
  expect(out.actions).toEqual([]);
  expect(JSON.stringify(out)).not.toContain("installation_id");
});

test("a hostile account cannot reach the policy URL through escaping", () => {
  const out = connectOutcome("not_your_workspace", { ws: "acme%2F..%2Fevil", installationId: "42" });
  expect(out.actions.some((a) => a.href.includes("oauth_application_policy"))).toBe(false);
});

test("nothing is emitted for a missing context at all", () => {
  const out = connectOutcome("not_your_workspace");
  expect(out.actions).toEqual([]);
  expect(out.message).toContain("that organization");
});
