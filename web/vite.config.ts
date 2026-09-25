/// <reference types="vitest/config" />
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The Go server the dev server proxies to: `go run ./cmd/gocov-preview`
// (in-memory store, fake sign-in with GOCOV_PREVIEW_AUTH=1).
const backend = process.env.GOCOV_BACKEND ?? "http://localhost:8099";

// Everything the Go server still answers itself: the page routes are the
// SPA's now. A key starting with "^" is matched as a regex, which is how the
// one endpoint that lives inside an SPA route shape — the raw profile
// download — is picked out without the pages around it.
const serverPaths = [
  "/api", "/oauth", "/logout", "/badge", "/static", "/healthz",
  "/robots.txt", "/sitemap.xml", "/github", "/workspace-connect",
  String.raw`^/uploads/\d+/profile$`,
];

export default defineConfig(({ command }) => ({
  // Built bundles are served by Go from /static/app/ (internal/server/spa.go).
  base: command === "build" ? "/static/app/" : "/",
  plugins: [react()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  build: { outDir: "../internal/webui/dist", emptyOutDir: true },
  server: { proxy: Object.fromEntries(serverPaths.map((p) => [p, { target: backend, changeOrigin: false }])) },
  // vmThreads builds jsdom once per worker instead of once per test file,
  // while still giving each file a fresh module graph and globals.
  test: {
    environment: "jsdom",
    pool: "vmThreads",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: false,
    // `npm run coverage` (CI uploads it to gocov as the "web" part). LCOV
    // paths are written relative to the repo root (web/src/...) so they
    // match the forge's paths with no -path-prefix.
    coverage: {
      provider: "v8",
      include: ["src/**/*.{ts,tsx}"],
      exclude: ["src/**/*.test.{ts,tsx}", "src/test/**"],
      reporter: ["text-summary", ["lcov", { projectRoot: ".." }]],
    },
  },
}));
