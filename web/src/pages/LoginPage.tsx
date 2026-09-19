import { useQuery } from "@tanstack/react-query";
import { Navigate, useSearchParams } from "react-router";
import { QueryBoundary } from "@/components/molecules";
import { LoginCard } from "@/components/organisms/LoginCard";
import { loginQuery, sessionQuery } from "@/lib/api/queries";
import { usePageTitle } from "@/lib/title";
import { routes } from "@/lib/urls";

/** Where sign-in returns to when nothing said otherwise. */
const HOME = "/";

/**
 * Only a path on this origin may be handed to the server as ?next=. "//host"
 * and "/\host" are scheme-relative URLs a browser would follow off-site.
 */
function safeNext(raw: string | null): string {
  if (raw === null || !raw.startsWith("/")) return HOME;
  if (raw.startsWith("//") || raw.startsWith("/\\")) return HOME;
  return raw;
}

/** The sign-in page, doubling as the access-denied and failed-sign-in page. */
export default function LoginPage() {
  const [params] = useSearchParams();
  const denied = params.get("denied") === "1";
  // The server's own redirect spells the generic failure ?error=1.
  const failed = params.get("failed") === "1" || params.get("error") === "1";
  const session = useQuery(sessionQuery());
  const query = useQuery(loginQuery(denied));
  usePageTitle("sign in");

  if (session.data?.user) return <Navigate to={routes.dashboard()} replace />;

  return (
    <QueryBoundary query={query}>
      {(info) => (
        <LoginCard
          info={info}
          next={safeNext(params.get("next"))}
          error={denied ? "denied" : failed ? "failed" : null}
        />
      )}
    </QueryBoundary>
  );
}
