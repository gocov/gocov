import type { ApiErrorBody } from "./types";

/** A non-2xx answer from /api/ui. `status` drives what the UI does next. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch("/api/ui" + path, {
    method,
    credentials: "same-origin",
    headers: body === undefined ? { Accept: "application/json" } : { Accept: "application/json", "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 204) return undefined as T;
  const data: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    const message = (data as ApiErrorBody | null)?.error ?? res.statusText;
    throw new ApiError(res.status, message);
  }
  return data as T;
}

export const apiGet = <T>(path: string) => request<T>("GET", path);
export const apiPost = <T>(path: string, body?: unknown) => request<T>("POST", path, body ?? {});

/**
 * A dead session starts over at /login, coming back to where the viewer was.
 * A full navigation rather than a router one: this runs from the query cache,
 * outside React, and the reload drops every stale answer with it.
 */
export function redirectToLogin(): void {
  const next = window.location.pathname + window.location.search;
  window.location.assign("/login?next=" + encodeURIComponent(next));
}
