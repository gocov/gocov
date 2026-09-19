import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { DangerCard } from "@/components/organisms/DangerCard";
import { GatesCard } from "@/components/organisms/GatesCard";
import { ReportingCard } from "@/components/organisms/ReportingCard";
import { TokenCard } from "@/components/organisms/TokenCard";
import { SettingsLayout, type SettingsNavItem } from "@/components/templates/SettingsLayout";
import { Button, Chip, InlineCode, Mono, Notice, Select, TextInput } from "@/components/atoms";
import { Card, FormField, PageHeader, QueryBoundary } from "@/components/molecules";
import { ApiError, apiPost } from "@/lib/api/client";
import { workspaceSettingsPath, workspaceSettingsQuery } from "@/lib/api/queries";
import type { TokenReveal, WorkspaceSettings, WorkspaceSettingsInput } from "@/lib/api/types";
import { plural } from "@/lib/format";
import { useUrlNotice } from "@/lib/notice";
import { retentionOptions, workspaceInput } from "@/lib/settings";
import { usePageTitle } from "@/lib/title";
import { routes } from "@/lib/urls";

/** The codes the connect redirects carry, said the way this page can act on them. */
const connectNotices = {
  connect_failed: "Connecting to the forge did not complete. Nothing was changed — try again from Reporting below.",
  connect_owners_only:
    "Connecting is a workspace owner’s move, and your last sign-in listed you as a member. Nothing was changed — " +
    "if you have become an admin since, sign in again and connect from Reporting below.",
};

export default function WorkspaceSettingsPage() {
  const params = useParams();
  const forge = params.forge ?? "";
  const prefix = params.prefix ?? "";
  const query = useQuery(workspaceSettingsQuery(forge, prefix));
  usePageTitle(`${prefix} settings`);
  return (
    <QueryBoundary query={query}>
      {(data) => <WorkspaceSettingsView key={`${forge}/${prefix}`} forge={forge} prefix={prefix} settings={data} />}
    </QueryBoundary>
  );
}

function WorkspaceSettingsView({ forge, prefix, settings }: { forge: string; prefix: string; settings: WorkspaceSettings }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const key = workspaceSettingsQuery(forge, prefix).queryKey;
  const path = workspaceSettingsPath(forge, prefix);

  // Gates and Defaults are one document with two Save buttons: the form is
  // shared, and whichever button is pressed posts all of it.
  const [form, setForm] = useState<WorkspaceSettingsInput>(() => workspaceInput(settings));
  const [pressed, setPressed] = useState("");
  const [savedIn, setSavedIn] = useState("");

  const { workspace: ws, owner } = settings;
  // Where the server sends the browser back after a grant or an install —
  // this page holds the Connect button the consent started from.
  const notice = useUrlNotice({ codes: connectNotices });

  const save = useMutation({
    mutationFn: (input: WorkspaceSettingsInput) => apiPost<WorkspaceSettings>(`${path}/settings`, input),
    onSuccess: (next) => {
      client.setQueryData(key, next);
      setForm(workspaceInput(next));
      setSavedIn(pressed);
    },
  });

  const disconnect = useMutation({
    mutationFn: () => apiPost<WorkspaceSettings>(`${path}/disconnect`),
    onSuccess: (next) => client.setQueryData(key, next),
  });

  const remove = useMutation({
    mutationFn: () => apiPost<void>(`${path}/delete`),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ["dashboard"] });
      void navigate(routes.dashboard());
    },
  });

  function update(patch: Partial<WorkspaceSettingsInput>) {
    setSavedIn("");
    setForm((f) => ({ ...f, ...patch }));
  }

  function submit(section: string) {
    setPressed(section);
    setSavedIn("");
    save.mutate(form);
  }

  /** Both editable cards carry the same button; only the pressed one says so. */
  function saveFooter(section: string, hint: string, ownerOnly: string) {
    if (!owner) return <span>{ownerOnly}</span>;
    return (
      <>
        <Button variant="primary" onClick={() => submit(section)} disabled={save.isPending} loading={save.isPending && pressed === section}>
          {save.isPending && pressed === section ? "Saving…" : "Save"}
        </Button>
        <span>{hint}</span>
        {savedIn === section && <Chip tone="good">Saved</Chip>}
      </>
    );
  }

  const nav: SettingsNavItem[] = [
    ...(settings.reporting.available ? [{ id: "reporting", label: "Reporting", group: "Workspace" }] : []),
    { id: "uploads", label: "Uploads", group: "Workspace" },
    { id: "gates", label: "Coverage gates", group: "Workspace" },
    { id: "defaults", label: "Defaults", group: "Workspace" },
    { id: "delete", label: "Delete workspace", group: "Danger zone" },
  ];

  const header = (
    <PageHeader
      title={
        <>
          Workspace <Mono>{prefix}</Mono>{" "}
          {settings.reporting.state === "broken" && <Chip tone="bad">Reporting broken</Chip>}
        </>
      }
      meta={
        <>
          {ws.forge_label}
          {settings.repo_count > 0 && <> · {plural(settings.repo_count, "repository", "repositories")}</>} ·{" "}
          <Link to={routes.workspaceSetup(forge, prefix)}>Setup instructions</Link>
        </>
      }
    />
  );

  return (
    <SettingsLayout header={header} nav={nav}>
      {notice && <Notice tone={notice.tone}>{notice.text}</Notice>}
      {!owner && (
        <Notice>
          You are a member of this workspace, so these settings are read-only. Changing them — and seeing the upload
          token — takes a workspace owner: an admin or owner of <Mono>{prefix}</Mono> on {ws.forge_label}. Roles refresh
          at every sign-in.
        </Notice>
      )}
      {save.isError && <Notice tone="bad">{save.error instanceof ApiError ? save.error.message : "The settings could not be saved."}</Notice>}

      {settings.reporting.available && (
        <SettingsLayout.Section id="reporting">
          <ReportingCard
            forge={ws.forge}
            forgeLabel={ws.forge_label}
            reporting={settings.reporting}
            owner={owner}
            repoCount={settings.repo_count}
            onDisconnect={() => disconnect.mutate()}
          />
        </SettingsLayout.Section>
      )}

      <SettingsLayout.Section id="uploads">
        <TokenCard
          title="Uploads"
          intro={
            <>
              Every repository under <InlineCode>{prefix}/</InlineCode> uploads with this token. Rotating it takes
              effect immediately — CI has to be updated in the same change.
            </>
          }
          serverUrl={settings.server_url}
          tokenMasked={settings.token_masked}
          owner={owner}
          onReveal={() => apiPost<TokenReveal>(`${path}/reveal-token`).then((r) => r.token)}
          onRotate={() =>
            apiPost<TokenReveal>(`${path}/rotate-token`).then((r) => {
              // The masked form in the cache is the old one now; the token
              // itself stays out of the cache.
              void client.invalidateQueries({ queryKey: key });
              return r.token;
            })
          }
        />
      </SettingsLayout.Section>

      <SettingsLayout.Section id="gates">
        <GatesCard
          gate={form.gate}
          scope="workspace"
          readOnly={!owner}
          onChange={(gate) => update({ gate })}
          footer={saveFooter("gates", "Applies to the next upload.", "Owners set the gates.")}
        />
      </SettingsLayout.Section>

      <SettingsLayout.Section id="defaults">
        <Card>
          <Card.Header title="Defaults" />
          <Card.Body>
            <div className="stack">
              <FormField label="Default branch" help="Trends and gate baselines are measured against this branch.">
                {(field) => (
                  <TextInput
                    {...field}
                    value={form.default_branch}
                    disabled={!owner}
                    onChange={(e) => update({ default_branch: e.target.value })}
                  />
                )}
              </FormField>
              <FormField label="Keep reports for" help="Older uploads are pruned; totals stay. Pruning is coming soon.">
                {(field) => (
                  <Select
                    {...field}
                    value={String(form.report_retention_days)}
                    disabled={!owner}
                    onChange={(e) => update({ report_retention_days: Number(e.target.value) })}
                  >
                    {retentionOptions.map((o) => (
                      <option key={o.value} value={o.value}>
                        {o.label}
                      </option>
                    ))}
                  </Select>
                )}
              </FormField>
            </div>
          </Card.Body>
          <Card.Footer>
            {saveFooter(
              "defaults",
              "Branch and gate defaults apply to repositories registered from now on.",
              "Owners set the defaults.",
            )}
          </Card.Footer>
        </Card>
      </SettingsLayout.Section>

      <SettingsLayout.Section id="delete">
        <DangerCard
          title="Delete workspace"
          actionLabel="Delete this workspace"
          hint="This cannot be undone."
          ownerOnlyHint="Only a workspace owner can delete it."
          owner={owner}
          confirmText={`Delete ${prefix} and all of its coverage data? This cannot be undone.`}
          onConfirm={() => remove.mutateAsync()}
        >
          <p>
            Removes <InlineCode>{prefix}</InlineCode>
            {settings.repo_count > 0 && <>, its {plural(settings.repo_count, "repository", "repositories")}</>} and
            every coverage report gocov holds for them. Uploads with this token start failing immediately. Nothing is
            changed on {ws.forge_label}.
          </p>
        </DangerCard>
      </SettingsLayout.Section>
    </SettingsLayout>
  );
}
