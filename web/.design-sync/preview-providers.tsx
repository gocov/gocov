// The context every preview card renders inside.
//
// Twenty of the components link somewhere (`Link` from react-router), and two
// read a TanStack query client. Both throw outside their provider, so a card
// without this wrapper renders blank rather than wrong — which is why it is
// wired as `provider` in .design-sync/config.json.
//
// A MemoryRouter, not a BrowserRouter: a preview card has no server behind it,
// and a click that pushed a real URL would navigate the card's iframe away.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { MemoryRouter } from "react-router";

// One client per mount, not one per module: a card renders several cells side
// by side, and a shared cache would answer them all from whichever fetched
// first — three cells of the same component would look identical on the grid
// even though each is right on its own. Retries are off and nothing refetches,
// so a preview never sits loading on a fetch that cannot succeed.
export function PreviewProviders({ children }: { children?: ReactNode }) {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false, staleTime: Infinity } },
      }),
  );
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}
