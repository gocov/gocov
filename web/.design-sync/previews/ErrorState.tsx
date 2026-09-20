import { ErrorState } from "gocov-web";

export function Retryable() {
  return <ErrorState message="The server did not answer in time." onRetry={() => {}} />;
}

export function WithoutRetry() {
  return <ErrorState message="acme/api could not be read: the GitHub grant for this workspace was revoked." />;
}
