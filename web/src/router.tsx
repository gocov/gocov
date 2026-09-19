import { lazy, Suspense } from "react";
import type { RouteObject } from "react-router";
import { AppShell } from "@/components/templates";
import DashboardPage from "@/pages/DashboardPage";
import LoginPage from "@/pages/LoginPage";
import NotFoundPage from "@/pages/NotFoundPage";
import OnboardingPage from "@/pages/OnboardingPage";
import RepoPage from "@/pages/RepoPage";
import RepoSettingsPage from "@/pages/RepoSettingsPage";
import SourcePage from "@/pages/SourcePage";
import UploadPage from "@/pages/UploadPage";
import WorkspaceSettingsPage from "@/pages/WorkspaceSettingsPage";
import WorkspaceSetupPage from "@/pages/WorkspaceSetupPage";

// The component gallery is the team's, not a visitor's: its own chunk, fetched
// only when someone opens /_components.
const ComponentsPage = lazy(() => import("@/pages/ComponentsPage"));

// The canonical URLs: the same shapes the Go server answers
// (internal/server/server.go), which serves this app's shell for every one of
// them. Slugs, workspace prefixes and file paths contain slashes and ride as the trailing splat:
// read them with useParams()["*"].
export const routes: RouteObject[] = [
  {
    element: <AppShell />,
    children: [
      { index: true, element: <DashboardPage /> },
      { path: "repos/:forge/*", element: <RepoPage /> },
      { path: "uploads/:id", element: <UploadPage /> },
      { path: "uploads/:id/files/*", element: <SourcePage /> },
      { path: "w/:forge/*", element: <DashboardPage /> },
      { path: "workspace-settings/:forge/*", element: <WorkspaceSettingsPage /> },
      { path: "workspace-setup/:forge/*", element: <WorkspaceSetupPage /> },
      { path: "onboarding", element: <OnboardingPage /> },
      { path: "repo-settings/:forge/*", element: <RepoSettingsPage /> },
      { path: "login", element: <LoginPage /> },
      {
        path: "_components",
        element: (
          <Suspense fallback={null}>
            <ComponentsPage />
          </Suspense>
        ),
      },
      { path: "*", element: <NotFoundPage /> },
    ],
  },
];
