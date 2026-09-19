import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { RouterProvider, createMemoryRouter } from "react-router";

/**
 * Renders a page the way the app does — inside a router and a query client —
 * at `path`, matched against `route` (e.g. "repos/:forge/*"). Stub the network
 * with mockApi() first.
 */
export function renderPage(element: ReactElement, { route = "*", path = "/" }: { route?: string; path?: string } = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const router = createMemoryRouter([{ path: route, element }], { initialEntries: [path] });
  return { router, ...render(<QueryClientProvider client={client}><RouterProvider router={router} /></QueryClientProvider>) };
}

type Handler = unknown | ((init: RequestInit | undefined) => unknown);

/**
 * Stubs fetch for /api/ui. Keys are "METHOD /path" with the path relative to
 * /api/ui and without the query string ("GET /dashboard"); a value is the
 * JSON body, or a function of the request. Unlisted calls answer 404. A value
 * of the shape {status, error} answers that error. Returns the fetch mock.
 */
export function mockApi(handlers: Record<string, Handler>) {
  const mock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost");
    const key = `${init?.method ?? "GET"} ${url.pathname.replace(/^\/api\/ui/, "")}`;
    const handler = handlers[key];
    if (handler === undefined) return Response.json({ error: "not found" }, { status: 404 });
    const body = typeof handler === "function" ? (handler as (i: RequestInit | undefined) => unknown)(init) : handler;
    if (body && typeof body === "object" && "status" in body && "error" in body) {
      const e = body as { status: number; error: string };
      return Response.json({ error: e.error }, { status: e.status });
    }
    return body === undefined ? new Response(null, { status: 204 }) : Response.json(body);
  });
  vi.stubGlobal("fetch", mock);
  return mock;
}
