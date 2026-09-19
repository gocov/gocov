import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router";
import { DangerCard } from "@/components/organisms/DangerCard";
import { GatesCard } from "@/components/organisms/GatesCard";
import { TokenCard } from "@/components/organisms/TokenCard";
import { SettingsLayout, type SettingsNavItem } from "@/components/templates/SettingsLayout";
import { Checkbox, Chip, InlineCode, Mono, Notice, TextInput, Textarea } from "@/components/atoms";
import { Breadcrumbs, Card, CopyField, FormField, PageHeader, QueryBoundary, SaveFooter } from "@/components/molecules";
import { ApiError, apiPost } from "@/lib/api/client";
import { repoSettingsPath, repoSettingsQuery } from "@/lib/api/queries";
import type { RepoSettings, RepoSettingsInput, TokenReveal } from "@/lib/api/types";
import { useSectionSave } from "@/lib/sectionSave";
import { ignorePatterns, patternLabel, repoInput } from "@/lib/settings";
import { usePageTitle } from "@/lib/title";
import { routes } from "@/lib/urls";

export default function RepoSettingsPage() {
  const params = useParams();
  const forge = params.forge ?? "";
  const slug = params["*"] ?? "";
  const query = useQuery(repoSettingsQuery(forge, slug));
  usePageTitle(`${slug} settings`);
  return (
    <QueryBoundary query={query}>
      {(data) => <RepoSettingsView key={`${forge}/${slug}`} forge={forge} slug={slug} settings={data} />}
    </QueryBoundary>
  );
}

function RepoSettingsView({ forge, slug, settings }: { forge: string; slug: string; settings: RepoSettings }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const key = repoSettingsQuery(forge, slug).queryKey;

  const { repo, workspace, owner } = settings;

  // Every editable card edits one document; each Save posts all of it.
  const { form, update, section, error } = useSectionSave({
    seed: () => repoInput(settings),
    post: (input: RepoSettingsInput) => apiPost<RepoSettings>(repoSettingsPath(forge, slug, "save"), input),
    onSaved: (next) => {
      client.setQueryData(key, next);
      return repoInput(next);
    },
  });

  const remove = useMutation({
    mutationFn: () => apiPost<void>(repoSettingsPath(forge, slug, "delete")),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ["dashboard"] });
      void navigate(routes.dashboard());
    },
  });

  const saveFooter = (id: string, hint: string, ownerOnly: string) => (
    <SaveFooter owner={owner} hint={hint} ownerOnly={ownerOnly} {...section(id)} />
  );

  const nav: SettingsNavItem[] = [
    { id: "general", label: "General", group: "Repository" },
    { id: "gates", label: "Coverage gates", group: "Repository" },
    { id: "ignore", label: "Ignored files", group: "Repository" },
    ...(settings.show_public_reports ? [{ id: "public-reports", label: "Public reports", group: "Repository" }] : []),
    { id: "uploads", label: "Uploads", group: "Repository" },
    { id: "badge", label: "Badge", group: "Repository" },
    { id: "remove", label: "Remove repository", group: "Danger zone" },
  ];

  const header = (
    <PageHeader
      breadcrumbs={
        <Breadcrumbs
          items={[
            { label: "Repositories", to: routes.dashboard() },
            { label: workspace.prefix, to: routes.workspace(workspace.forge, workspace.prefix) },
            { label: <Mono>{slug}</Mono>, to: routes.repo(forge, slug) },
            { label: "Settings" },
          ]}
        />
      }
      title={
        <>
          Settings <Mono>{slug}</Mono>
        </>
      }
    />
  );

  return (
    <SettingsLayout header={header} nav={nav}>
      {!owner && (
        <Notice>
          You are a member of <Mono>{workspace.prefix}</Mono>, so these settings are read-only. Changing them — and
          seeing the upload token — takes a workspace owner: an admin or owner of the workspace on the forge. Roles
          refresh at every sign-in.
        </Notice>
      )}
      {error !== null && <Notice tone="bad">{error instanceof ApiError ? error.message : "The settings could not be saved."}</Notice>}

      <SettingsLayout.Section id="general">
        <Card>
          <Card.Header title="General" />
          <Card.Body>
            <div className="stack">
              <p>
                The base branch is what every other branch is compared against — trends and gate baselines are measured
                from it.
              </p>
              <FormField label="Base branch" help="Defaults to the branch detected at registration.">
                {(field) => (
                  <TextInput
                    {...field}
                    value={form.default_branch}
                    disabled={!owner}
                    onChange={(e) => update({ default_branch: e.target.value })}
                  />
                )}
              </FormField>
            </div>
          </Card.Body>
          <Card.Footer>{saveFooter("general", "Applies to the next upload.", "Owners set the base branch.")}</Card.Footer>
        </Card>
      </SettingsLayout.Section>

      <SettingsLayout.Section id="gates">
        <GatesCard
          gate={form.gate}
          scope="repo"
          readOnly={!owner}
          onChange={(gate) => update({ gate })}
          footer={saveFooter(
            "gates",
            "Applies to uploads received from now on. Past verdicts are not recalculated.",
            "Owners set the gates.",
          )}
        />
      </SettingsLayout.Section>

      <SettingsLayout.Section id="ignore">
        <Card>
          <Card.Header
            title="Ignored files"
            actions={
              <Chip tone={ignorePatterns(form.ignore_paths).length === 0 ? "neutral" : "accent"}>
                {patternLabel(form.ignore_paths)}
              </Chip>
            }
          />
          <Card.Body>
            <div className="stack">
              <p>
                Files matching these patterns are left out of every report — generated code, mocks, a dev harness —
                before totals, diff coverage and the gate are computed. One pattern per line, matched at any directory
                level of the paths shown in reports: <InlineCode>*</InlineCode> stays within one directory,{" "}
                <InlineCode>**</InlineCode> crosses them, a directory covers everything under it, and a leading{" "}
                <InlineCode>/</InlineCode> pins the pattern to the root.
              </p>
              <FormField
                label="Ignore patterns"
                help="A pattern matching every file is refused at upload time rather than landing a 0% report."
              >
                {(field) => (
                  <Textarea
                    {...field}
                    mono
                    spellCheck={false}
                    placeholder={"cmd/preview/**\n**/*.pb.go\n*_mock.go"}
                    value={form.ignore_paths}
                    disabled={!owner}
                    onChange={(e) => update({ ignore_paths: e.target.value })}
                  />
                )}
              </FormField>
            </div>
          </Card.Body>
          <Card.Footer>
            {saveFooter(
              "ignore",
              "Applies to uploads received from now on. Past reports keep their numbers.",
              "Owners set the ignore patterns.",
            )}
          </Card.Footer>
        </Card>
      </SettingsLayout.Section>

      {settings.show_public_reports && (
        <SettingsLayout.Section id="public-reports">
          <Card>
            <Card.Header
              title="Public reports"
              actions={form.public_reports ? <Chip tone="good">On</Chip> : <Chip tone="plain">Off</Chip>}
            />
            <Card.Body>
              <div className="stack">
                <p>
                  The forge reports this repository as public, so its report pages — the repo overview, upload reports
                  and source views — can be read without signing in. Settings and every mutating action stay owner-only
                  either way.
                </p>
                <Checkbox
                  label="Serve read-only report pages to visitors who are not signed in"
                  checked={form.public_reports}
                  disabled={!owner}
                  onChange={(e) => update({ public_reports: e.target.checked })}
                />
              </div>
            </Card.Body>
            <Card.Footer>
              {saveFooter(
                "public-reports",
                "Turning this off closes the pages to members only immediately.",
                "Owners decide.",
              )}
            </Card.Footer>
          </Card>
        </SettingsLayout.Section>
      )}

      <SettingsLayout.Section id="uploads">
        <TokenCard
          title="Uploads"
          intro={
            <>
              Builds for <InlineCode>{slug}</InlineCode> can upload with this repository token. Rotating it takes effect
              immediately — CI has to be updated in the same change.
            </>
          }
          serverUrl={null}
          tokenMasked={settings.token_masked}
          owner={owner}
          onReveal={() => apiPost<TokenReveal>(repoSettingsPath(forge, slug, "reveal-token")).then((r) => r.token)}
          onRotate={() =>
            apiPost<TokenReveal>(repoSettingsPath(forge, slug, "rotate-token")).then((r) => {
              // The cached masked form is the old token's now.
              void client.invalidateQueries({ queryKey: key });
              return r.token;
            })
          }
        />
      </SettingsLayout.Section>

      <SettingsLayout.Section id="badge">
        <Card>
          <Card.Header title="Badge" actions={<Chip tone="good">Public</Chip>} />
          <Card.Body>
            <div className="stack">
              <p>
                The badge shows total coverage on the base branch. Anyone with the link can load it, without seeing the
                reports behind it.
              </p>
              <CopyField
                label="Badge markdown"
                value={repo.badge_markdown}
                preview={<img src={repo.badge_url} alt="coverage badge" />}
              />
            </div>
          </Card.Body>
        </Card>
      </SettingsLayout.Section>

      <SettingsLayout.Section id="remove">
        <DangerCard
          title="Remove repository"
          actionLabel="Remove this repository"
          hint="This cannot be undone."
          ownerOnlyHint="Only a workspace owner can remove it."
          owner={owner}
          confirmText={`Remove ${slug} and all of its coverage data? This cannot be undone.`}
          onConfirm={() => remove.mutateAsync()}
        >
          <p>
            Removes <InlineCode>{slug}</InlineCode> from gocov along with its uploads and every report behind them.
            Uploads with its token start failing immediately. Nothing is changed on the forge — the repository itself,
            and any branch protection referring to the gocov check, stay as they are.
          </p>
        </DangerCard>
      </SettingsLayout.Section>
    </SettingsLayout>
  );
}
