import { QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider, createBrowserRouter } from "react-router";
import { ApiError, redirectToLogin } from "@/lib/api/client";
import { routes } from "./router";
import "./styles/base.css";

const queryClient = new QueryClient({
  queryCache: new QueryCache({
    // The session ended (or never began): start over at the sign-in page.
    onError: (error) => {
      if (error instanceof ApiError && error.status === 401) redirectToLogin();
    },
  }),
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchOnWindowFocus: false,
      // A 4xx is an answer, not a glitch.
      retry: (count, error) => !(error instanceof ApiError && error.status < 500) && count < 2,
    },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={createBrowserRouter(routes)} />
    </QueryClientProvider>
  </StrictMode>,
);
