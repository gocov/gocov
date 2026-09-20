import { Breadcrumbs, Mono } from "gocov-web";

export function ThreeLevels() {
  return (
    <Breadcrumbs
      items={[
        { label: "acme", to: "/" },
        { label: <Mono>acme/api</Mono>, to: "/" },
        { label: <Mono>internal/server/upload.go</Mono> },
      ]}
    />
  );
}

export function WorkspaceAndRepo() {
  return (
    <Breadcrumbs
      items={[
        { label: "acme", to: "/" },
        { label: <Mono>acme/api</Mono> },
      ]}
    />
  );
}

export function UploadTrail() {
  return (
    <Breadcrumbs
      items={[
        { label: "acme", to: "/" },
        { label: <Mono>acme/api</Mono>, to: "/" },
        { label: "Uploads", to: "/" },
        { label: "Upload 412" },
      ]}
    />
  );
}
