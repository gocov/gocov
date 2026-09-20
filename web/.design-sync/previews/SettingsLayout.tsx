import {
  Breadcrumbs,
  Card,
  Chip,
  FormField,
  InlineCode,
  Mono,
  Notice,
  PageHeader,
  SaveFooter,
  SecretField,
  SettingsLayout,
  Textarea,
  TextInput,
} from "gocov-web";

// Each cell carries the page's whole nav — that side column is the template —
// and as many sections as fit the card, the way the real pages compose them.

const saveFooter = (hint, ownerOnly, owner = true) => (
  <SaveFooter owner={owner} hint={hint} ownerOnly={ownerOnly} busy={false} saving={false} saved={false} onSave={() => {}} />
);

function gatesCard(owner = true) {
  return (
    <Card>
      <Card.Header title="Coverage gates" />
      <Card.Body>
        <div className="stack">
          <p className="muted small">A gate turns an upload into a verdict. Leave a field empty to switch that rule off.</p>
          <FormField label="Minimum total coverage" help="The whole repository, statement-weighted.">
            {(field) => <TextInput {...field} defaultValue="80" disabled={!owner} />}
          </FormField>
          <FormField label="Minimum diff coverage" help="Lines the pull request touched.">
            {(field) => <TextInput {...field} defaultValue="70" disabled={!owner} />}
          </FormField>
        </div>
      </Card.Body>
      <Card.Footer>
        {saveFooter("Applies to uploads received from now on. Past verdicts are not recalculated.", "Owners set the gates.", owner)}
      </Card.Footer>
    </Card>
  );
}

export function WorkspaceSettings() {
  return (
    <SettingsLayout
      header={
        <PageHeader
          title={
            <>
              Workspace <Mono>acme</Mono>
            </>
          }
          meta={
            <>
              GitHub · 12 repositories · <a href="/workspace-setup/github/acme">Setup instructions</a>
            </>
          }
        />
      }
      nav={[
        { id: "reporting", label: "Reporting", group: "Workspace" },
        { id: "uploads", label: "Uploads", group: "Workspace" },
        { id: "gates", label: "Coverage gates", group: "Workspace" },
        { id: "defaults", label: "Defaults", group: "Workspace" },
        { id: "delete", label: "Delete workspace", group: "Danger zone" },
      ]}
    >
      <SettingsLayout.Section id="reporting">
        <Card>
          <Card.Header title="Reporting" actions={<Chip tone="good">Connected</Chip>} />
          <Card.Body>
            <p>
              Statuses and pull request comments post as <Mono>gocov[bot]</Mono> across all 12 repositories under{" "}
              <InlineCode>acme/</InlineCode>.
            </p>
          </Card.Body>
        </Card>
      </SettingsLayout.Section>

      <SettingsLayout.Section id="uploads">
        <Card>
          <Card.Header title="Uploads" />
          <Card.Body>
            <div className="stack">
              <p>
                Every repository under <InlineCode>acme/</InlineCode> uploads with this token. Rotating it takes effect
                immediately — CI has to be updated in the same change.
              </p>
              <SecretField
                name="GOCOV_TOKEN"
                kind="Secret"
                note="Shown to workspace owners only"
                masked="gocov_live_••••••••••••"
                onReveal={() => Promise.resolve("gocov_live_9f2c41d8a7b3")}
              />
            </div>
          </Card.Body>
        </Card>
      </SettingsLayout.Section>

      <SettingsLayout.Section id="delete">
        <Card danger>
          <Card.Header title="Delete workspace" />
          <Card.Body>Removes acme, its 12 repositories and every coverage report gocov holds for them.</Card.Body>
        </Card>
      </SettingsLayout.Section>
    </SettingsLayout>
  );
}

export function RepositorySettings() {
  return (
    <SettingsLayout
      header={
        <PageHeader
          breadcrumbs={
            <Breadcrumbs
              items={[
                { label: "Repositories", to: "/" },
                { label: "acme", to: "/" },
                { label: <Mono>acme/api</Mono>, to: "/" },
                { label: "Settings" },
              ]}
            />
          }
          title={
            <>
              Settings <Mono>acme/api</Mono>
            </>
          }
        />
      }
      nav={[
        { id: "general", label: "General", group: "Repository" },
        { id: "gates", label: "Coverage gates", group: "Repository" },
        { id: "ignore", label: "Ignored files", group: "Repository" },
        { id: "public-reports", label: "Public reports", group: "Repository" },
        { id: "badge", label: "Badge", group: "Repository" },
        { id: "remove", label: "Remove repository", group: "Danger zone" },
      ]}
    >
      <SettingsLayout.Section id="general">
        <Card>
          <Card.Header title="General" />
          <Card.Body>
            <FormField label="Base branch" help="Trends and gate baselines are measured against this branch.">
              {(field) => <TextInput {...field} defaultValue="main" />}
            </FormField>
          </Card.Body>
          <Card.Footer>{saveFooter("Applies to the next upload.", "Owners set the base branch.")}</Card.Footer>
        </Card>
      </SettingsLayout.Section>

      <SettingsLayout.Section id="ignore">
        <Card>
          <Card.Header title="Ignored files" />
          <Card.Body>
            <FormField label="Ignore paths" help="One pattern per line. Matched against the paths in the profile.">
              {(field) => <Textarea {...field} mono defaultValue={"vendor/**\n**/*_test.go\ninternal/mock/**"} />}
            </FormField>
          </Card.Body>
          <Card.Footer>
            {saveFooter("Applies to uploads received from now on. Past reports keep their numbers.", "Owners set the ignore patterns.")}
          </Card.Footer>
        </Card>
      </SettingsLayout.Section>
    </SettingsLayout>
  );
}

export function ReadOnlyForAMember() {
  return (
    <SettingsLayout
      header={
        <PageHeader
          title={
            <>
              Workspace <Mono>acme</Mono>
            </>
          }
          meta="GitHub · 12 repositories"
        />
      }
      nav={[
        { id: "reporting", label: "Reporting", group: "Workspace" },
        { id: "uploads", label: "Uploads", group: "Workspace" },
        { id: "gates", label: "Coverage gates", group: "Workspace" },
        { id: "defaults", label: "Defaults", group: "Workspace" },
        { id: "delete", label: "Delete workspace", group: "Danger zone" },
      ]}
    >
      <Notice>
        You are a member of this workspace, so these settings are read-only. Changing them — and seeing the upload token
        — takes a workspace owner: an admin or owner of <Mono>acme</Mono> on GitHub. Roles refresh at every sign-in.
      </Notice>

      <SettingsLayout.Section id="gates">{gatesCard(false)}</SettingsLayout.Section>
    </SettingsLayout>
  );
}
