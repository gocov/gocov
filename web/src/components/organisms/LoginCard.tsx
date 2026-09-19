import { Link } from "react-router";
import { ForgeMark, Notice } from "@/components/atoms";
import { Card } from "@/components/molecules";
import type { Forge, LoginInfo } from "@/lib/api/types";
import { routes, server } from "@/lib/urls";
import "./LoginCard.css";

/** The ring, as the app bar wears it. Copied from AppShell — atoms own no mark. */
function Mark() {
  return (
    <svg viewBox="0 0 32 32" width="34" height="34" aria-hidden="true">
      <circle cx="16" cy="16" r="12.5" fill="none" stroke="var(--border)" strokeWidth="5" />
      <circle cx="16" cy="16" r="12.5" fill="none" stroke="var(--accent)" strokeWidth="5" strokeLinecap="round" strokeDasharray="61.3 78.5" transform="rotate(-90 16 16)" />
    </svg>
  );
}

const forgeLabels: Record<string, string> = { github: "GitHub", gitlab: "GitLab", bitbucket: "Bitbucket" };

const forgeLabel = (forge: Forge) => forgeLabels[forge] ?? forge.charAt(0).toUpperCase() + forge.slice(1);

/**
 * "a, b on GitHub; c on GitLab" — the workspaces grouped by the forge they
 * are on, in the order the server listed them.
 */
export function trackedText(workspaces: LoginInfo["tracked_workspaces"]): string {
  const groups: { forge: Forge; names: string[] }[] = [];
  for (const ws of workspaces) {
    const group = groups.find((g) => g.forge === ws.forge);
    if (group) group.names.push(ws.name);
    else groups.push({ forge: ws.forge, names: [ws.name] });
  }
  return groups.map((g) => `${g.names.join(", ")} on ${forgeLabel(g.forge)}`).join("; ");
}

/** Sign-in: one button per forge this instance offers, and why you are here. */
export function LoginCard({
  info,
  next,
  error,
}: {
  info: LoginInfo;
  next: string;
  error: "denied" | "failed" | null;
}) {
  return (
    <div className="LoginCard">
      <Card>
        <Card.Body>
          <div className="stack">
            <div className="LoginCard__head">
              <Mark />
              <h1>Sign in to gocov</h1>
            </div>

            {error === "denied" && (
              <Notice tone="bad">
                <div className="stack stack-1">
                  <p>
                    Your account has no access to this instance. Sign-in is limited to members of the workspaces and
                    organizations tracked here; ask the operator to add you to one of them.
                  </p>
                  {info.tracked_workspaces.length > 0 && (
                    <p className="muted small">Tracked workspaces: {trackedText(info.tracked_workspaces)}</p>
                  )}
                </div>
              </Notice>
            )}
            {error === "failed" && <Notice tone="bad">Sign-in did not complete. Please try again.</Notice>}

            <p className="muted">
              {info.hosted
                ? "Sign in to get started — you can register your workspace right after. No separate password is needed."
                : "Use the account that is a member of this instance’s workspaces. No separate password is needed."}
            </p>

            {info.providers.length > 0 ? (
              <div className="LoginCard__providers">
                {info.providers.map((provider) => (
                  <a key={provider.name} className="LoginCard__provider" href={server.oauthStart(provider.name, next)}>
                    <ForgeMark forge={provider.name} size={16} />
                    Sign in with {provider.label}
                  </a>
                ))}
              </div>
            ) : (
              <Notice>
                Sign-in is not configured on this instance. <Link to={routes.dashboard()}>Go to the dashboard</Link>
              </Notice>
            )}

            <p className="LoginCard__foot">
              {info.hosted
                ? "Your first workspace is registered right after sign-in."
                : "Access is granted by workspace membership. Ask a workspace admin if you cannot get in."}
            </p>
          </div>
        </Card.Body>
      </Card>
    </div>
  );
}
