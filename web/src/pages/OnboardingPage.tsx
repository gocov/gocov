import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { LinkButton, Notice } from "@/components/atoms";
import { QueryBoundary } from "@/components/molecules";
import { WorkspacePicker } from "@/components/organisms/WorkspacePicker";
import { ApiError, apiPost } from "@/lib/api/client";
import { onboardingQuery } from "@/lib/api/queries";
import type { OnboardingInfo, RegisterInput, RegisterResult } from "@/lib/api/types";
import { connectOutcome, type ConnectOutcome } from "@/lib/connect";
import { usePageTitle } from "@/lib/title";
import { routes } from "@/lib/urls";
import "./OnboardingPage.css";
import { track, type EventProps } from "@/lib/analytics";

/**
 * The first screen: where gocov lives. One choice, then the dashboard —
 * the setup checklist takes it from there. An instance with no
 * registration answers 404, and the boundary's not-found panel says so.
 */
export default function OnboardingPage() {
  const query = useQuery(onboardingQuery());
  const connect = useConnectOutcome();
  usePageTitle("set up coverage");
  return (
    <div className="OnboardingPage stack">
      {connect && <ConnectNotice outcome={connect} />}
      <QueryBoundary query={query}>{(info) => <ChooseWorkspace info={info} />}</QueryBoundary>
    </div>
  );
}

/**
 * Reads ?connect= (with ?ws= and ?installation_id=) once, then takes all
 * three back out of the URL: an install that ended here is a one-shot
 * message, so a reload or a shared link does not replay it — while the
 * message itself stays for as long as the page is open.
 */
function useConnectOutcome(): ConnectOutcome | null {
  const [params, setParams] = useSearchParams();

  // Captured on the first render, before the effect below strips the query.
  const [outcome] = useState<ConnectOutcome | null>(() => {
    const code = params.get("connect");
    if (!code) return null;
    return connectOutcome(code, { ws: params.get("ws"), installationId: params.get("installation_id") });
  });

  useEffect(() => {
    if (!params.has("connect")) return;
    const next = new URLSearchParams(params);
    next.delete("connect");
    next.delete("ws");
    next.delete("installation_id");
    setParams(next, { replace: true });
  }, [params, setParams]);

  return outcome;
}

/** What the deleted connect page used to be: the state, in words, with its ways out. */
function ConnectNotice({ outcome }: { outcome: ConnectOutcome }) {
  return (
    <Notice tone={outcome.tone}>
      <div className="stack stack-1">
        <p>
          <strong>{outcome.title}</strong>
        </p>
        <p>{outcome.message}</p>
        {outcome.actions.length > 0 && (
          <div className="row">
            {outcome.actions.map((action) => (
              <LinkButton key={action.href} href={action.href} external={action.external} size="sm">
                {action.label}
              </LinkButton>
            ))}
          </div>
        )}
      </div>
    </Notice>
  );
}

function ChooseWorkspace({ info }: { info: OnboardingInfo }) {
  const client = useQueryClient();
  const navigate = useNavigate();

  const register = useMutation({
    mutationFn: (prefix: string) => apiPost<RegisterResult>("/onboarding/register", { prefix } satisfies RegisterInput),
    onSuccess: async (result) => {
      await Promise.all([
        client.invalidateQueries({ queryKey: ["dashboard"] }),
        client.invalidateQueries({ queryKey: ["onboarding"] }),
      ]);
      void navigate(routes.dashboard(`${result.forge}/${result.prefix}`));
    },
  });

  return (
    <div className="stack">
      {register.isError && (
        <Notice tone="bad">
          {register.error instanceof ApiError ? register.error.message : "The workspace could not be registered."}
        </Notice>
      )}
      <WorkspacePicker
        info={info}
        busy={register.isPending ? (register.variables ?? null) : null}
        onRegister={(prefix) => register.mutate(prefix)}
        onEvent={(event, props) => track(event, props as EventProps)}
      />
    </div>
  );
}
