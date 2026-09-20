import { Breadcrumbs, Button, Chip, Mono, PageHeader } from "gocov-web";

export function BreadcrumbsTitleMetaAndActions() {
  return (
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
  );
}

export function TitleAlone() {
  return <PageHeader title="Repositories" />;
}

export function TitleAndMeta() {
  return <PageHeader title={<Mono>acme/api</Mono>} meta="74.0% on main · last upload 3 hours ago" />;
}

export function TitleAndActions() {
  return (
    <PageHeader
      title="acme"
      actions={
        <>
          <Button>Settings</Button>
          <Button variant="primary">Add a repository</Button>
        </>
      }
    />
  );
}
