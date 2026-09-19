import { act, render } from "@testing-library/react";
import { createElement } from "react";
import { RouterProvider, createMemoryRouter } from "react-router";
import type { AnalyticsConfig } from "./analytics";

// Each test gets a fresh module: the loader is a one-shot, and its state
// (started, the pending queue) is module-level by design.
const fresh = async () => {
  vi.resetModules();
  return await import("./analytics");
};

const config: AnalyticsConfig = { key: "phc_test", host: "https://eu.i.posthog.example", user_id: "42" };

const scriptTag = () => document.head.querySelector<HTMLScriptElement>('script[src*="array.js"]');

/** The stub the loader finds on window once the script "arrives". */
function fakePostHog() {
  return {
    init: vi.fn(),
    identify: vi.fn(),
    capture: vi.fn(),
    startSessionRecording: vi.fn(),
    stopSessionRecording: vi.fn(),
  };
}

function arrive(ph = fakePostHog()) {
  vi.stubGlobal("posthog", ph);
  scriptTag()?.onload?.(new Event("load"));
  return ph;
}

afterEach(() => {
  vi.unstubAllGlobals();
  document.head.querySelectorAll('script[src*="array.js"]').forEach((s) => s.remove());
});

test("an instance without a key loads no third-party script and swallows events", async () => {
  const a = await fresh();
  a.initAnalytics(undefined);
  a.initAnalytics({ key: "", host: "" });
  expect(scriptTag()).toBeNull();
  expect(() => a.track("copy_snippet_clicked", { forge: "github" })).not.toThrow();
});

test("a configured key loads the PostHog array loader from the configured host", async () => {
  const a = await fresh();
  a.initAnalytics(config);
  const script = scriptTag();
  expect(script?.src).toBe("https://eu.i.posthog.example/static/array.js");
  expect(script?.async).toBe(true);

  // A second call must not load it twice.
  a.initAnalytics(config);
  expect(document.head.querySelectorAll('script[src*="array.js"]')).toHaveLength(1);
});

test("init keeps the narrow options app.js used, and identifies by id", async () => {
  const a = await fresh();
  a.initAnalytics(config);
  const ph = arrive();

  expect(ph.init).toHaveBeenCalledTimes(1);
  const [key, options] = ph.init.mock.calls[0] as [string, Record<string, unknown>];
  expect(key).toBe("phc_test");
  expect(options).toMatchObject({
    api_host: "https://eu.i.posthog.example",
    persistence: "memory",
    autocapture: false,
    enable_heatmaps: false,
    capture_pageview: false,
    capture_pageleave: true,
    respect_dnt: true,
    person_profiles: "identified_only",
  });
  expect(options.session_recording).toMatchObject({
    maskAllInputs: true,
    blockClass: "ph-no-capture",
    blockSelector: "[data-ph-no-capture]",
  });
  // Never the email: the numeric gocov id, namespaced.
  expect(ph.identify).toHaveBeenCalledWith("user:42");
});

test("the dashboard is never recorded: it names private repositories", async () => {
  vi.stubGlobal("location", { pathname: "/", search: "?ws=github%2Facme" });
  const a = await fresh();
  a.initAnalytics(config);
  const ph = arrive();
  expect((ph.init.mock.calls[0] as [string, Record<string, unknown>])[1].disable_session_recording).toBe(true);
});

test("session replay is off outside the setup pages", async () => {
  vi.stubGlobal("location", { pathname: "/repos/github/acme/api", search: "" });
  const a = await fresh();
  a.initAnalytics(config);
  const ph = arrive();
  expect((ph.init.mock.calls[0] as [string, Record<string, unknown>])[1].disable_session_recording).toBe(true);
});

test.each(["/onboarding", "/workspaces/github/acme/setup"])(
  "session replay is on for %s, where someone is being set up",
  async (pathname) => {
    vi.stubGlobal("location", { pathname, search: "" });
    const a = await fresh();
    a.initAnalytics(config);
    const ph = arrive();
    expect((ph.init.mock.calls[0] as [string, Record<string, unknown>])[1].disable_session_recording).toBe(false);
  },
);

test("events fired before the script lands are delivered once it does", async () => {
  const a = await fresh();
  a.initAnalytics(config);
  a.track("copy_snippet_clicked", { forge: "github", auth: "oidc", language: "go" });

  const ph = arrive();
  expect(ph.capture).toHaveBeenCalledWith(
    "copy_snippet_clicked",
    { forge: "github", auth: "oidc", language: "go" },
    undefined,
  );
});

test("a later event goes out straight away, so a navigation cannot lose it", async () => {
  const a = await fresh();
  a.initAnalytics(config);
  const ph = arrive();
  a.track("set_gate_clicked", { forge: "github" });
  expect(ph.capture).toHaveBeenCalledWith("set_gate_clicked", { forge: "github" }, {
    transport: "sendBeacon",
    send_instantly: true,
  });
});

test("a broken PostHog never breaks the page it measures", async () => {
  const a = await fresh();
  a.initAnalytics(config);
  const ph = fakePostHog();
  ph.capture.mockImplementation(() => {
    throw new Error("network");
  });
  arrive(ph);
  expect(() => a.track("first_upload_received")).not.toThrow();
});

test("every route the app enters is a pageview, and ?ref rides along", async () => {
  const a = await fresh();
  a.initAnalytics(config);
  const ph = arrive();

  const Probe = () => {
    a.usePageviews();
    return null;
  };
  const router = createMemoryRouter([{ path: "*", element: createElement(Probe) }], {
    initialEntries: ["/?ref=badge"],
  });
  render(createElement(RouterProvider, { router }));

  expect(ph.capture).toHaveBeenCalledWith("$pageview", { ref: "badge" }, undefined);
  ph.capture.mockClear();

  await act(() => router.navigate("/repos/github/acme/api"));
  expect(ph.capture).toHaveBeenCalledWith("$pageview", undefined, undefined);
  // Off the setup pages, recording stops with the route.
  expect(ph.stopSessionRecording).toHaveBeenCalled();
});

test("an event before any init is dropped, not queued forever", async () => {
  const a = await fresh();
  a.track("language_selected", { language: "ruby" });
  a.initAnalytics(config);
  const ph = arrive();
  expect(ph.capture).not.toHaveBeenCalled();
});
