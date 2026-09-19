import { useState, type ReactNode } from "react";
import {
  Avatar,
  Button,
  Checkbox,
  Chip,
  CodeBlock,
  CoverageBar,
  CoverageFigure,
  Delta,
  ForgeMark,
  Icon,
  InlineCode,
  LinkButton,
  Mono,
  Notice,
  Select,
  Sparkline,
  Spinner,
  TextInput,
  Textarea,
  Toggle,
  Tooltip,
  type IconName,
} from "@/components/atoms";
import {
  Banner,
  BeforeAfter,
  Breadcrumbs,
  Card,
  ConfirmDialog,
  CopyButton,
  CopyField,
  EmptyState,
  ErrorState,
  FormField,
  GateRow,
  IdentityRow,
  KeyValue,
  KeyValueList,
  NotFoundState,
  OptionRow,
  PageHeader,
  Pagination,
  SecretField,
  SectionHeader,
  SegmentedControl,
  Skeleton,
  StatRow,
  StatTile,
  Toolbar,
  UncoveredRanges,
} from "@/components/molecules";
import { usePageTitle } from "@/lib/title";
import "./ComponentsPage.css";

const iconNames: IconName[] = [
  "check",
  "cross",
  "caret-down",
  "caret-right",
  "arrow-up",
  "arrow-down",
  "arrow-left",
  "arrow-right",
  "minus",
  "search",
  "external",
  "copy",
  "eye",
  "eye-off",
  "folder",
  "file",
  "warning",
  "info",
  "person",
  "bot",
];

/** One specimen with the state it is in. */
function Spec({ caption, wide, children }: { caption: string; wide?: boolean; children: ReactNode }) {
  return (
    <div className={`Spec${wide ? " Spec--wide" : ""}`}>
      <span className="Spec__caption">{caption}</span>
      <div className="Spec__stage">{children}</div>
    </div>
  );
}

/** One component, with every specimen of it. */
function Demo({ title, note, children }: { title: string; note?: string; children: ReactNode }) {
  return (
    <section className="Demo">
      <h3 className="Demo__title">
        {title}
        {note !== undefined && <span className="Demo__note">{note}</span>}
      </h3>
      <div className="Demo__specs">{children}</div>
    </section>
  );
}

export default function ComponentsPage() {
  const [filter, setFilter] = useState("all");
  const [notify, setNotify] = useState(true);
  const [publicReports, setPublicReports] = useState(false);
  const [retention, setRetention] = useState("90");
  const [minCoverage, setMinCoverage] = useState<number | null>(80);
  const [minDiff, setMinDiff] = useState<number | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [deleted, setDeleted] = useState(false);
  usePageTitle("components");

  return (
    <div className="Components stack stack-3">
      <PageHeader
        title="Components"
        meta="Every atom and molecule, in every state it has. The page a change to the design system is reviewed on."
      />

      <SectionHeader title="Atoms" id="atoms" />

      <Demo title="Icon" note="16px grid, one stroke path, currentColor">
        <Spec caption="the whole set" wide>
          <div className="Components__icons">
            {iconNames.map((name) => (
              <span key={name} className="Components__icon">
                <Icon name={name} size={16} />
                <span className="Components__iconName">{name}</span>
              </span>
            ))}
          </div>
        </Spec>
      </Demo>

      <Demo title="Button / LinkButton">
        <Spec caption="default">
          <Button>Rotate token</Button>
        </Spec>
        <Spec caption="primary">
          <Button variant="primary">Save</Button>
        </Spec>
        <Spec caption="danger">
          <Button variant="danger">Delete</Button>
        </Spec>
        <Spec caption="quiet">
          <Button variant="quiet">Sign out</Button>
        </Spec>
        <Spec caption="with icon, small">
          <Button size="sm" icon="copy">
            Copy
          </Button>
        </Spec>
        <Spec caption="disabled">
          <Button disabled>Unavailable</Button>
        </Spec>
        <Spec caption="link in the app">
          <LinkButton to="/">Dashboard</LinkButton>
        </Spec>
        <Spec caption="link out of it">
          <LinkButton href="https://docs.gocov.dev" external icon="external">
            Docs
          </LinkButton>
        </Spec>
      </Demo>

      <Demo title="Chip" note="status is a dot, never a tinted pill">
        <Spec caption="good">
          <Chip tone="good">Gate passing</Chip>
        </Spec>
        <Spec caption="warn">
          <Chip tone="warn">Stale</Chip>
        </Spec>
        <Spec caption="bad">
          <Chip tone="bad">Gate failing</Chip>
        </Spec>
        <Spec caption="accent">
          <Chip tone="accent">3 active</Chip>
        </Spec>
        <Spec caption="neutral">
          <Chip>No gate</Chip>
        </Spec>
        <Spec caption="plain (no dot)">
          <Chip tone="plain">App install</Chip>
        </Spec>
      </Demo>

      <Demo title="CoverageBar">
        <Spec caption="good">
          <CoverageBar value={84.2} />
        </Spec>
        <Spec caption="warn">
          <CoverageBar value={63} />
        </Spec>
        <Spec caption="bad">
          <CoverageBar value={41.4} />
        </Spec>
        <Spec caption="empty">
          <CoverageBar value={0} />
        </Spec>
      </Demo>

      <Demo title="CoverageFigure">
        <Spec caption="lg / good">
          <CoverageFigure value={84.2} />
        </Spec>
        <Spec caption="lg / warn">
          <CoverageFigure value={62.5} />
        </Spec>
        <Spec caption="lg / bad">
          <CoverageFigure value={38} />
        </Spec>
        <Spec caption="md">
          <CoverageFigure value={74} size="md" />
        </Spec>
        <Spec caption="no report yet">
          <CoverageFigure value={null} />
        </Spec>
      </Demo>

      <Demo title="Delta">
        <Spec caption="up">
          <Delta value={2.4} />
        </Spec>
        <Spec caption="down">
          <Delta value={-1.1} />
        </Spec>
        <Spec caption="flat">
          <Delta value={0.02} />
        </Spec>
        <Spec caption="no baseline">
          <Delta value={null} />
        </Spec>
      </Demo>

      <Demo title="Sparkline" note="76×22, oldest point first">
        <Spec caption="rising">
          <Sparkline series={[61, 63, 62, 66, 69, 74]} />
        </Spec>
        <Spec caption="falling">
          <Sparkline series={[80, 79, 76, 71, 70, 66]} />
        </Spec>
        <Spec caption="flat">
          <Sparkline series={[70, 70.2, 69.9, 70, 70.1]} />
        </Spec>
        <Spec caption="constant">
          <Sparkline series={[70, 70, 70]} />
        </Spec>
        <Spec caption="stale (the series stopped)">
          <Sparkline series={[74, 72, 73, 70]} stale />
        </Spec>
        <Spec caption="one point">
          <Sparkline series={[70]} />
        </Spec>
      </Demo>

      <Demo title="TextInput">
        <Spec caption="text">
          <TextInput aria-label="Default branch" defaultValue="main" />
        </Spec>
        <Spec caption="search">
          <TextInput type="search" aria-label="Search files" placeholder="Search files…" />
        </Spec>
        <Spec caption="number">
          <TextInput type="number" aria-label="Minimum coverage" defaultValue={80} />
        </Spec>
        <Spec caption="invalid">
          <TextInput aria-label="Branch" defaultValue="mian" invalid />
        </Spec>
        <Spec caption="disabled">
          <TextInput aria-label="Locked" defaultValue="owners only" disabled />
        </Spec>
      </Demo>

      <Demo title="Textarea">
        <Spec caption="plain" wide>
          <Textarea aria-label="Notes" defaultValue="Anything a person types by the paragraph." />
        </Spec>
        <Spec caption="mono" wide>
          <Textarea aria-label="Ignore paths" mono defaultValue={"vendor/**\n**/*_test.go\ninternal/mock/**"} />
        </Spec>
      </Demo>

      <Demo title="Select">
        <Spec caption="native, styled">
          <Select aria-label="Keep reports for" value={retention} onChange={(e) => setRetention(e.target.value)}>
            <option value="90">90 days</option>
            <option value="365">1 year</option>
            <option value="0">Forever</option>
          </Select>
        </Spec>
        <Spec caption="disabled">
          <Select aria-label="Locked" defaultValue="90" disabled>
            <option value="90">90 days</option>
          </Select>
        </Spec>
      </Demo>

      <Demo title="Checkbox">
        <Spec caption="controlled">
          <Checkbox
            label="Public reports"
            checked={publicReports}
            onChange={(e) => setPublicReports(e.target.checked)}
          />
        </Spec>
        <Spec caption="disabled">
          <Checkbox label="Not available here" disabled />
        </Spec>
      </Demo>

      <Demo title="Toggle">
        <Spec caption="on">
          <Toggle checked={notify} onChange={setNotify} label="Post statuses back" />
        </Spec>
        <Spec caption="off">
          <Toggle checked={!notify} onChange={(v) => setNotify(!v)} label="Mirror of the one beside it" />
        </Spec>
        <Spec caption="disabled">
          <Toggle checked onChange={() => {}} label="Owners only" disabled />
        </Spec>
      </Demo>

      <Demo title="Spinner">
        <Spec caption="on its own">
          <Spinner />
        </Spec>
        <Spec caption="in a sentence">
          <span className="row">
            <Spinner label="Rotating" /> Rotating the upload token…
          </span>
        </Spec>
      </Demo>

      <Demo title="Avatar">
        <Spec caption="forge">
          <Avatar kind="forge" forge="github" />
        </Spec>
        <Spec caption="initial">
          <Avatar name="acme" />
        </Spec>
        <Spec caption="person">
          <Avatar kind="person" />
        </Spec>
        <Spec caption="bot">
          <Avatar kind="bot" />
        </Spec>
        <Spec caption="larger">
          <Avatar kind="forge" forge="gitlab" size={40} />
        </Spec>
      </Demo>

      <Demo title="ForgeMark" note="the brand paths the sign-in buttons use">
        <Spec caption="github">
          <ForgeMark forge="github" size={20} label="GitHub" />
        </Spec>
        <Spec caption="gitlab (optically larger)">
          <ForgeMark forge="gitlab" size={20} label="GitLab" />
        </Spec>
        <Spec caption="bitbucket">
          <ForgeMark forge="bitbucket" size={20} label="Bitbucket" />
        </Spec>
      </Demo>

      <Demo title="Mono · InlineCode · CodeBlock">
        <Spec caption="Mono">
          <Mono>a1b2c3d4e5f6 · acme/api · main</Mono>
        </Spec>
        <Spec caption="InlineCode">
          <span>
            Set <InlineCode>GOCOV_TOKEN</InlineCode> in your CI secrets.
          </span>
        </Spec>
        <Spec caption="CodeBlock (scrolls sideways)" wide>
          <CodeBlock label="GitHub Actions step">
            {"- uses: gocov/gocov-action@v1\n  with:\n    files: coverage.out\n    token: ${{ secrets.GOCOV_TOKEN }}"}
          </CodeBlock>
        </Spec>
      </Demo>

      <Demo title="Notice" note="hairline border, a tone mark, no tinted background">
        <Spec caption="neutral" wide>
          <Notice>Coverage still uploads; only posting back to the forge has stopped.</Notice>
        </Spec>
        <Spec caption="good" wide>
          <Notice tone="good">Connected. Statuses and comments post as gocov[bot].</Notice>
        </Spec>
        <Spec caption="warn" wide>
          <Notice tone="warn">No uploads in 21 days — this repo's coverage is stale.</Notice>
        </Spec>
        <Spec caption="bad" wide>
          <Notice tone="bad">The grant was revoked on GitLab. Nothing has been posted back since.</Notice>
        </Spec>
        <Spec caption="busy" wide>
          <Notice busy>Rotating the upload token…</Notice>
        </Spec>
      </Demo>

      <Demo title="Tooltip" note="hover or focus the underlined word">
        <Spec caption="on a word">
          <span className="row">
            Coverage is{" "}
            <Tooltip text="Statement-weighted across every repository in the workspace.">
              <span className="Components__dotted">statement-weighted</span>
            </Tooltip>
          </span>
        </Spec>
        <Spec caption="on an icon">
          <Tooltip text="This repository has no gate configured.">
            <Icon name="info" size={16} />
          </Tooltip>
        </Spec>
      </Demo>

      <SectionHeader title="Molecules" id="molecules" />

      <Demo title="PageHeader">
        <Spec caption="breadcrumbs, mono title, meta and actions" wide>
          <PageHeader
            breadcrumbs={
              <Breadcrumbs
                items={[
                  { label: "acme", to: "/" },
                  { label: "acme/api", to: "/" },
                  { label: "Upload 412" },
                ]}
              />
            }
            title={
              <>
                Upload <Mono>412</Mono> <Chip tone="good">Gate passing</Chip>
              </>
            }
            meta="main · a1b2c3d · 3 hours ago · GitHub Actions"
            actions={
              <>
                <Button icon="external">Download profile</Button>
                <Button variant="primary">Settings</Button>
              </>
            }
          />
        </Spec>
        <Spec caption="title alone" wide>
          <PageHeader title="Repositories" />
        </Spec>
      </Demo>

      <Demo title="Breadcrumbs">
        <Spec caption="three levels, the last is the page" wide>
          <Breadcrumbs
            items={[
              { label: "acme", to: "/" },
              { label: <Mono>acme/api</Mono>, to: "/" },
              { label: <Mono>internal/server/upload.go</Mono> },
            ]}
          />
        </Spec>
      </Demo>

      <Demo title="SectionHeader">
        <Spec caption="with a note" wide>
          <SectionHeader title="Files">28 files · 4 changed</SectionHeader>
        </Spec>
        <Spec caption="with a control" wide>
          <SectionHeader title="Uploads">
            <Button size="sm">All branches</Button>
          </SectionHeader>
        </Spec>
      </Demo>

      <Demo title="StatRow / StatTile">
        <Spec caption="four tiles, one card, hairline dividers" wide>
          <StatRow>
            <StatTile label="Coverage" value={<CoverageFigure value={74} size="md" />} hint="statement-weighted" />
            <StatTile label="Gates passing" value="6/8" />
            <StatTile label="Stale" value="1" hint="no upload in 14 days" />
            <StatTile label="Reporting" value={<Chip tone="good">Connected</Chip>} hint="gocov[bot]" />
          </StatRow>
        </Spec>
        <Spec caption="two tiles" wide>
          <StatRow>
            <StatTile label="Statements" value="12,481" />
            <StatTile label="Covered" value="9,236" />
          </StatRow>
        </Spec>
      </Demo>

      <Demo title="SegmentedControl">
        <Spec caption="counts, one disabled" wide>
          <SegmentedControl
            label="Filter files"
            value={filter}
            onChange={setFilter}
            options={[
              { value: "all", label: "All", count: 28 },
              { value: "changed", label: "Changed", count: 4 },
              { value: "source", label: "Source changed", count: 2 },
              { value: "coverage", label: "Coverage changed", disabled: true },
            ]}
          />
        </Spec>
      </Demo>

      <Demo title="Toolbar">
        <Spec caption="controls left, status right" wide>
          <Toolbar
            label="File tools"
            left={
              <>
                <TextInput type="search" aria-label="Search files" placeholder="Search files…" />
                <SegmentedControl
                  label="View mode"
                  value={filter === "all" ? "tree" : "list"}
                  onChange={(v) => setFilter(v === "tree" ? "all" : "changed")}
                  options={[
                    { value: "tree", label: "Tree" },
                    { value: "list", label: "List" },
                  ]}
                />
              </>
            }
            right="28 files"
          />
        </Spec>
      </Demo>

      <Demo title="KeyValueList">
        <Spec caption="stacked" wide>
          <KeyValueList>
            <KeyValue label="Received" value="3 hours ago" />
            <KeyValue label="Profile" value={<Mono>coverage.out · 184 kB</Mono>} />
            <KeyValue label="Format" value="go" />
          </KeyValueList>
        </Spec>
        <Spec caption="inline" wide>
          <KeyValueList layout="inline">
            <KeyValue label="Uploaded by" value={<Mono>gocov-action v1.17.0</Mono>} />
            <KeyValue label="CI run" value="GitHub Actions #2184" />
            <KeyValue label="Parts" value="3 of 3 merged" />
          </KeyValueList>
        </Spec>
      </Demo>

      <Demo title="CopyButton / CopyField">
        <Spec caption="button">
          <CopyButton value="gocov upload coverage.out" />
        </Spec>
        <Spec caption="small, custom label">
          <CopyButton size="sm" label="Copy markdown" value="![coverage](https://gocov.dev/badge/acme/api)" />
        </Spec>
        <Spec caption="field with a preview" wide>
          <CopyField
            label="Badge markdown"
            value="![coverage](https://app.gocov.dev/badge/github/acme/api)"
            preview={<Chip tone="good">coverage 74.0%</Chip>}
          />
        </Spec>
      </Demo>

      <Demo title="SecretField">
        <Spec caption="secret — reveal fetches it once" wide>
          <SecretField
            name="GOCOV_TOKEN"
            kind="Secret"
            note="Shown to workspace owners only"
            masked="gocov_live_••••••••••••"
            onReveal={() => new Promise((resolve) => setTimeout(() => resolve("gocov_live_9f2c41d8a7b3"), 400))}
          />
        </Spec>
        <Spec caption="variable — plain value" wide>
          <SecretField
            name="GOCOV_SERVER"
            kind="Variable"
            note="Your instance — not a secret"
            value="https://gocov.example.com"
          />
        </Spec>
        <Spec caption="locked (a member, not an owner)" wide>
          <SecretField name="GOCOV_TOKEN" kind="Secret" note="Shown to workspace owners only" locked />
        </Spec>
      </Demo>

      <Demo title="IdentityRow">
        <Spec caption="a bot" wide>
          <IdentityRow
            avatar={<Avatar kind="bot" />}
            id="gocov[bot]"
            description="Posting through the app install · 8 repositories"
            chip={<Chip tone="plain">App install</Chip>}
          />
        </Spec>
        <Spec caption="a person, grant broken" wide>
          <IdentityRow
            avatar={<Avatar kind="person" />}
            id="@omer"
            description="Grant revoked on GitLab"
            chip={<Chip tone="bad">Inactive</Chip>}
          />
        </Spec>
      </Demo>

      <Demo title="OptionRow">
        <Spec caption="a workspace to register" wide>
          <OptionRow
            avatar={<Avatar kind="forge" forge="gitlab" />}
            name="acme-labs"
            status="4 repositories · not registered"
            action={<Button variant="primary">Register</Button>}
          />
        </Spec>
        <Spec caption="already registered" wide>
          <OptionRow
            avatar={<Avatar kind="forge" forge="bitbucket" />}
            name="acme"
            status="12 repositories · registered 6 Sep"
            action={<Button>Open</Button>}
          />
        </Spec>
      </Demo>

      <Demo title="GateRow">
        <Spec caption="on, off, and read-only" wide>
          <div className="stack stack-1">
            <GateRow
              name="Minimum total coverage"
              help="Fails when the whole project drops below this figure."
              value={minCoverage}
              onChange={setMinCoverage}
            />
            <GateRow
              name="Minimum diff coverage"
              help="Fails when the lines changed in a pull request are covered below this figure."
              value={minDiff}
              onChange={setMinDiff}
              whenOn={70}
            />
            <GateRow
              name="Maximum coverage drop"
              help="Fails when total coverage falls by more than this against the base branch."
              value={2}
              onChange={() => {}}
              readOnly
            />
          </div>
        </Spec>
      </Demo>

      <Demo title="FormField">
        <Spec caption="label, help" wide>
          <FormField label="Default branch" help="Trends and gate baselines are measured against this branch.">
            {(field) => <TextInput {...field} defaultValue="main" />}
          </FormField>
        </Spec>
        <Spec caption="with an error" wide>
          <FormField
            label="Ignore paths"
            help="One pattern per line."
            error="“vendor/**/” is not a valid pattern."
          >
            {(field) => <Textarea {...field} mono defaultValue={"vendor/**/"} />}
          </FormField>
        </Spec>
      </Demo>

      <Demo title="BeforeAfter · UncoveredRanges">
        <Spec caption="before → after">
          <BeforeAfter before={71.2} after={74} />
        </Spec>
        <Spec caption="new file">
          <BeforeAfter before={null} after={62.5} />
        </Spec>
        <Spec caption="ranges" wide>
          <UncoveredRanges ranges="12-18, 40, 55-57, 61, 70-74, 88, 91, 104-110, 119, 130" max={6} />
        </Spec>
        <Spec caption="nothing uncovered">
          <UncoveredRanges ranges="" />
        </Spec>
      </Demo>

      <Demo title="Card">
        <Spec caption="header, body, footer" wide>
          <Card>
            <Card.Header title="Uploads" actions={<Chip tone="plain">Owner</Chip>} />
            <Card.Body>Every repository under acme/ uploads with this token.</Card.Body>
            <Card.Footer>
              <Button>Rotate token</Button>
              <span>The old token stops working the moment you rotate.</span>
            </Card.Footer>
          </Card>
        </Spec>
        <Spec caption="a table sits flush and scrolls" wide>
          <Card>
            <Card.Header title="Recent uploads" />
            <Card.Body flush>
              <table>
                <thead>
                  <tr>
                    <th>Commit</th>
                    <th>Branch</th>
                    <th>Coverage</th>
                    <th className="hide-sm">Δ</th>
                    <th className="hide-sm">When</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td>
                      <Mono>a1b2c3d</Mono>
                    </td>
                    <td>
                      <Mono>main</Mono>
                    </td>
                    <td>
                      <CoverageBar value={74} />
                    </td>
                    <td className="hide-sm">
                      <Delta value={2.8} />
                    </td>
                    <td className="hide-sm">3 hours ago</td>
                  </tr>
                  <tr>
                    <td>
                      <Mono>9f2c41d</Mono>
                    </td>
                    <td>
                      <Mono>fix/upload</Mono>
                    </td>
                    <td>
                      <CoverageBar value={62.5} />
                    </td>
                    <td className="hide-sm">
                      <Delta value={-1.1} />
                    </td>
                    <td className="hide-sm">yesterday</td>
                  </tr>
                </tbody>
              </table>
            </Card.Body>
          </Card>
        </Spec>
        <Spec caption="danger" wide>
          <Card danger>
            <Card.Header title="Delete workspace" />
            <Card.Body>
              Removes acme, its 12 repositories and every coverage report gocov holds for them.
            </Card.Body>
            <Card.Footer>
              <Button variant="danger" onClick={() => setConfirming(true)}>
                Delete this workspace
              </Button>
              <span>This cannot be undone.</span>
            </Card.Footer>
          </Card>
        </Spec>
      </Demo>

      <Demo title="ConfirmDialog" note="native <dialog>: Escape closes, focus returns to the trigger">
        <Spec caption="danger confirmation" wide>
          <div className="row">
            <Button variant="danger" onClick={() => setConfirming(true)}>
              Delete this workspace
            </Button>
            {deleted && <Chip tone="bad">Confirmed</Chip>}
          </div>
        </Spec>
      </Demo>

      <Demo title="Pagination">
        <Spec caption="first page" wide>
          <Pagination older={{ to: "/_components" }} />
        </Spec>
        <Spec caption="in the middle" wide>
          <Pagination newer={{ to: "/_components" }} older={{ to: "/_components" }} />
        </Spec>
        <Spec caption="last page" wide>
          <Pagination newer={{ to: "/_components" }} older={{ disabled: true }} />
        </Spec>
      </Demo>

      <Demo title="Banner">
        <Spec caption="neutral" wide>
          <Banner action={<Button size="sm">Set it up</Button>}>
            Sign-in is not configured: this instance is open to anyone who can reach it.
          </Banner>
        </Spec>
        <Spec caption="warn, dismissible for the session" wide>
          <Banner tone="warn" id="components-demo" dismissible>
            Reporting to Bitbucket stopped working — statuses and comments are being skipped.
          </Banner>
        </Spec>
      </Demo>

      <Demo title="EmptyState · QueryBoundary states">
        <Spec caption="empty" wide>
          <EmptyState message="No uploads on this branch yet." action={<Button variant="primary">Set up CI</Button>} />
        </Spec>
        <Spec caption="loading skeleton" wide>
          <Skeleton />
        </Spec>
        <Spec caption="not found" wide>
          <NotFoundState />
        </Spec>
        <Spec caption="error" wide>
          <ErrorState message="The server did not answer in time." onRetry={() => {}} />
        </Spec>
      </Demo>

      <ConfirmDialog
        open={confirming}
        danger
        title="Delete acme?"
        confirmLabel="Delete workspace"
        onConfirm={() => {
          setConfirming(false);
          setDeleted(true);
        }}
        onCancel={() => setConfirming(false)}
      >
        Every coverage report gocov holds for its 12 repositories goes too. This cannot be undone.
      </ConfirmDialog>
    </div>
  );
}
