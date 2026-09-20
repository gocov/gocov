import { ConfirmDialog } from "gocov-web";

export function DangerOpen() {
  return (
    <ConfirmDialog
      open
      danger
      title="Delete acme?"
      confirmLabel="Delete workspace"
      onConfirm={() => {}}
      onCancel={() => {}}
    >
      Every coverage report gocov holds for its 12 repositories goes too. This cannot be undone.
    </ConfirmDialog>
  );
}

export function PlainOpen() {
  return (
    <ConfirmDialog
      open
      title="Rotate the upload token for acme?"
      confirmLabel="Rotate token"
      cancelLabel="Keep the current token"
      onConfirm={() => {}}
      onCancel={() => {}}
    >
      Every CI job still using <code className="mono">GOCOV_TOKEN</code> stops uploading the moment the new token is issued.
    </ConfirmDialog>
  );
}
