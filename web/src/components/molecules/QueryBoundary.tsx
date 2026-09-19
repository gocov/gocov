import type { UseQueryResult } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { ApiError } from "@/lib/api/client";
import { Button } from "../atoms";
import "./QueryBoundary.css";

export function Skeleton({ lines = 4 }: { lines?: number }) {
  return (
    <div className="Skeleton" role="status" aria-label="Loading">
      {Array.from({ length: lines }, (_, i) => (
        <span key={i} className="Skeleton__line" style={{ width: `${90 - i * 12}%` }} />
      ))}
    </div>
  );
}

export function NotFoundState() {
  return (
    <div className="State stack stack-1">
      <p className="State__title">We couldn&rsquo;t find that page.</p>
      <p>The link may be out of date, or the repository it pointed to is no longer visible to your account.</p>
    </div>
  );
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="State stack stack-1" role="alert">
      <p className="State__title">Something went wrong.</p>
      <p>{message}</p>
      {onRetry && (
        <div>
          <Button onClick={onRetry}>Try again</Button>
        </div>
      )}
    </div>
  );
}

/**
 * The one way a page waits for its data: skeleton while pending, the 404
 * panel for a missing-or-hidden resource, a retryable error otherwise.
 * A 401 renders nothing — the app is already on its way to sign-in.
 */
export function QueryBoundary<T>({ query, children }: { query: UseQueryResult<T>; children: (data: T) => ReactNode }) {
  if (query.isPending) return <Skeleton />;
  if (query.isError) {
    const status = query.error instanceof ApiError ? query.error.status : 0;
    if (status === 401) return null;
    if (status === 404) return <NotFoundState />;
    return <ErrorState message={query.error.message} onRetry={() => void query.refetch()} />;
  }
  return <>{children(query.data)}</>;
}
