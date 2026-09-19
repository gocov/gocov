// The GitHub App install flow's terminal states. `/github/setup` decides in
// Go — it is the only thing that knows the installation's real account — and
// sends the browser to /onboarding?connect=<code>[&ws=][&installation_id=].
// The sentence and the ways out of each state are written here, the prose the
// deleted connect.html page used to carry.
//
// Everything that arrives is query text: anyone can type any of it. A value
// is used in an href only once it looks like what it claims to be, and an
// account name is only spoken back once it could be a GitHub login.

import { server } from "./urls";

export type ConnectTone = "neutral" | "warn" | "bad";

export interface ConnectAction {
  label: string;
  href: string;
  /** Leaves gocov — opens in a new tab. */
  external?: boolean;
}

export interface ConnectOutcome {
  tone: ConnectTone;
  title: string;
  message: string;
  actions: ConnectAction[];
}

/** A GitHub login: letters, digits and hyphens, up to 39 characters. */
const LOGIN = /^[A-Za-z0-9-]{1,39}$/;

/** An installation id: digits, and few enough of them to be a number. */
const INSTALLATION = /^[0-9]{1,19}$/;

/** Where GitHub sends the browser back to, for another run at the same install. */
const setupPath = (id: string) => `/github/setup?installation_id=${id}`;

/**
 * The org's third-party OAuth-app policy page. When an installed org is
 * absent from the user's sign-in org list, the usual cause is that the org
 * restricts third-party OAuth apps and has not approved gocov's — so it never
 * appears in the account's organizations and no amount of re-auth helps. This
 * takes an owner straight to the setting that allows gocov.
 */
function orgPolicy(login: string | null): ConnectAction[] {
  if (login === null) return [];
  return [
    {
      label: `Allow gocov on ${login}`,
      href: `https://github.com/organizations/${login}/settings/oauth_application_policy`,
      external: true,
    },
  ];
}

/**
 * Re-runs GitHub sign-in and comes back to this same installation. A
 * brand-new org is missing from the sign-in snapshot the claim gate checks
 * (the OAuth token is dropped at login); re-auth refreshes the snapshot, the
 * setup handler runs again with the same installation, and the claim goes
 * through — no dead end.
 */
function signInAgain(id: string | null): ConnectAction[] {
  if (id === null) return [];
  return [{ label: "Sign in again", href: server.oauthStart("github", setupPath(id)) }];
}

export interface ConnectContext {
  /** The forge account the install is about, as the server named it. */
  ws?: string | null;
  /** The GitHub installation the browser came back with. */
  installationId?: string | null;
}

/**
 * What to tell someone whom GitHub sent back without a connected workspace.
 * Pure: the code and the context are the whole input, so the page only has to
 * draw the result.
 */
export function connectOutcome(code: string, { ws, installationId }: ConnectContext = {}): ConnectOutcome {
  const login = ws && LOGIN.test(ws) ? ws : null;
  const id = installationId && INSTALLATION.test(installationId) ? installationId : null;
  // Named when the server named it plausibly, described when it did not.
  const account = login ?? "that organization";

  switch (code) {
    case "install_requested":
      return {
        tone: "neutral",
        title: "Install requested",
        message:
          "Your request went to the organization’s owners. Once one of them approves the installation, " +
          "GitHub brings them back here and the workspace is connected.",
        actions: [],
      };

    case "no_installation":
      return {
        tone: "warn",
        title: "No installation to connect",
        message:
          "That address is where GitHub returns after the gocov app is installed; it does nothing on its " +
          "own. Start from “Install the gocov app” below.",
        actions: [],
      };

    case "install_unconfirmed":
      return {
        tone: "bad",
        title: "GitHub did not confirm the installation",
        message:
          "The installation could not be verified with GitHub. If you have just installed the app, give " +
          "it a moment and try again.",
        actions: id === null ? [] : [{ label: "Try again", href: setupPath(id) }],
      };

    case "not_your_workspace":
      return {
        tone: "bad",
        title: "Not your workspace",
        message:
          `gocov can see the app installed on ${account}, but your GitHub sign-in did not list ` +
          `${account} among your organizations. Most often that means ${account} restricts third-party ` +
          "OAuth apps and has not approved gocov’s, so it stays hidden however many times you sign in. " +
          `If ${account} is yours, allow gocov there and then sign in again. If you only just created ` +
          "it, signing in again may be enough on its own.",
        actions: [...orgPolicy(login), ...signInAgain(id)],
      };

    case "owners_only":
      return {
        tone: "warn",
        title: "Owners only",
        message:
          `Connecting ${account} is a workspace owner’s move, and your last sign-in listed you as a ` +
          `member of ${account}, not an admin. If you have become one since, sign in again to refresh ` +
          "your role and come back here.",
        actions: signInAgain(id),
      };

    default:
      return {
        tone: "bad",
        title: "Install not completed",
        message: "The GitHub App install could not be completed.",
        actions: [],
      };
  }
}
