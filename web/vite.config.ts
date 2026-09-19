/// <reference types="vitest/config" />
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The Go server the dev server proxies to: `go run ./cmd/gocov-preview`
// (in-memory store, fake sign-in with GOCOV_PREVIEW_AUTH=1).
const backend = process.env.GOCOV_BACKEND ?? "http://localhost:8099";

// Everything the Go server still answers itself: the page routes are the
// SPA's now. A key starting with "^" is matched as a regex, which is how the
// two endpoints that live inside SPA route shapes — the raw profile download
// and the forge consent start — are picked out without the pages around them.
const serverPaths = [
  "/api", "/oauth", "/logout", "/badge", "/static", "/healthz",
  "/robots.txt", "/sitemap.xml", "/github",
  String.raw`^/uploads/\d+/profile$`,
  String.raw`^/workspaces/[^/]+/[^/]+/connect$`,
];

export default defineConfig(({ command }) => ({
  // Built bundles are served by Go from /static/app/ (internal/server/spa.go).
  base: command === "build" ? "/static/app/" : "/",
  plugins: [react()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  build: { outDir: "../internal/webui/dist", emptyOutDir: true },
  server: { proxy: Object.fromEntries(serverPaths.map((p) => [p, { target: backend, changeOrigin: false }])) },
  test: { environment: "jsdom", globals: true, setupFiles: ["./src/test/setup.ts"], css: false },
}));
