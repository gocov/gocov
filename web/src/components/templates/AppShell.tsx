import { useQuery } from "@tanstack/react-query";
import { useEffect } from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router";
import { initAnalytics, usePageviews } from "@/lib/analytics";
import { sessionQuery } from "@/lib/api/queries";
import { routes, server } from "@/lib/urls";
import { Button, Icon, Logo } from "../atoms";
import "./AppShell.css";

async function signOut() {
  await fetch(server.logout(), { method: "POST", credentials: "same-origin" });
  window.location.assign("/");
}

/** The frame every page sits in: one quiet bar, one content column. */
export function AppShell() {
  const { data: session } = useQuery(sessionQuery());
  const location = useLocation();
  // Off unless the instance configured it; then pageviews follow the router.
  useEffect(() => {
    initAnalytics(session?.analytics);
  }, [session?.analytics]);
  usePageviews();
  return (
    <div className="AppShell">
      <header className="AppShell__bar">
        <nav className="AppShell__nav" aria-label="Main">
          <NavLink to="/" className="AppShell__brand">
            <Logo /> gocov
          </NavLink>
          <NavLink to="/" end className="AppShell__link">
            Repositories
          </NavLink>
          <span className="spacer" />
          <a className="AppShell__link row hide-sm" href={server.docs()} target="_blank" rel="noopener">
            Docs <Icon name="external" size={12} />
          </a>
          {session?.user && (
            <>
              <span className="muted hide-sm" title={session.user.email}>
                {session.user.display_name}
              </span>
              <Button variant="quiet" size="sm" onClick={() => void signOut()}>
                Sign out
              </Button>
            </>
          )}
          {session && session.auth_enabled && !session.user && (
            <Link className="AppShell__link" to={routes.login(location.pathname + location.search)}>
              Sign in
            </Link>
          )}
        </nav>
      </header>
      {session && !session.auth_enabled && (
        <p className="AppShell__notice">
          Sign-in is not configured: this instance is open to anyone who can reach it.{" "}
          <a href={server.docs("sign-in/")} target="_blank" rel="noopener">
            Set it up
          </a>
        </p>
      )}
      <main className="AppShell__main">
        <Outlet />
      </main>
    </div>
  );
}
