// PostHog, ported from internal/server/static/app.js. The setup is
// deliberately narrow — no cookie or localStorage (memory persistence), no
// autocapture, Do Not Track honoured — and signed-in users are identified by
// their numeric gocov id, never by email. Session replay runs only on the
// pages worth watching someone go through (the dashboard, where setup now
// happens, and the two snippet pages), with every input masked and anything
// carrying data-ph-no-capture (the token fields) blocked; report, upload and
// source pages are never recorded.
//
// Nothing loads unless the server said so: the key arrives in
// GET /api/ui/session as `analytics`, which is present only when the
// operator configured GOCOV_POSTHOG_KEY.

import { useEffect } from "react";
import { useLocation } from "react-router";
import type { Session } from "./api/types";

export type AnalyticsConfig = NonNullable<Session["analytics"]>;

export type EventProps = Record<string, string | number | boolean | undefined>;

interface PostHog {
  init(key: string, options: Record<string, unknown>): void;
  identify(id: string): void;
  capture(event: string, props?: EventProps, options?: Record<string, unknown>): void;
  startSessionRecording?(): void;
  stopSessionRecording?(): void;
}

declare global {
  interface Window {
    posthog?: PostHog;
  }
}

/**
 * The pages worth watching someone go through, and nothing that names a
 * repository or renders source: choosing a workspace, and the page that hands
 * out a snippet. The dashboard is deliberately NOT here even though the setup
 * card lives on it — it lists private repository names, and the card's own
 * events (copy_snippet_clicked, first_upload_help_shown, first_upload_received)
 * already tell that story. The PostHog project's
 * `session_recording_url_trigger_config` must list exactly these patterns, so
 * a page outside them is never recorded even if this drifts.
 */
const replayPaths = /^\/onboarding\/?$|^\/workspace-setup\/[^/]+\/.+$/;

/** Set as soon as a key arrives, so a second call cannot load the script twice. */
let started = false;
/** The loaded client; until then events wait in `pending`. */
let client: PostHog | null = null;
/** Events fired while the script was still loading. Capped: it is telemetry. */
let pending: { event: string; props?: EventProps }[] = [];

const MAX_PENDING = 20;

function send(event: string, props?: EventProps, immediate = false): void {
  if (!started) return;
  if (!client) {
    if (pending.length < MAX_PENDING) pending.push({ event, props });
    return;
  }
  try {
    client.capture(event, props, immediate ? { transport: "sendBeacon", send_instantly: true } : undefined);
  } catch {
    // Telemetry never breaks the page it measures.
  }
}

/**
 * Loads PostHog and identifies the viewer. Idempotent and safe to call with
 * undefined — an instance with no key loads no third-party script at all.
 */
export function initAnalytics(analytics: AnalyticsConfig | null | undefined): void {
  if (started || !analytics?.key || !analytics.host) return;
  started = true;

  const replay = replayPaths.test(window.location.pathname);
  const script = document.createElement("script");
  script.src = analytics.host + "/static/array.js";
  script.async = true;
  script.onload = () => {
    const ph = window.posthog;
    if (!ph) return;
    try {
      ph.init(analytics.key, {
        api_host: analytics.host,
        persistence: "memory",
        // The app shows private repo names and source; no click text, no
        // heatmaps. JS errors are fine: message and stack only.
        autocapture: false,
        enable_heatmaps: false,
        capture_exceptions: true,
        disable_session_recording: !replay,
        // blockClass is what app.js used; the SPA marks token elements with
        // the data attribute instead (see molecules/SecretField).
        session_recording: { maskAllInputs: true, blockClass: "ph-no-capture", blockSelector: "[data-ph-no-capture]" },
        capture_pageview: false,
        // Page leaves give PostHog bounce rate and time on page; with memory
        // persistence they still stay within the one page load.
        capture_pageleave: true,
        // Core Web Vitals (LCP, INP, CLS): performance numbers, no content.
        capture_performance: { web_vitals: true },
        respect_dnt: true,
        person_profiles: "identified_only",
      });
      if (analytics.user_id) ph.identify("user:" + analytics.user_id);
    } catch {
      return;
    }
    client = ph;
    const queued = pending;
    pending = [];
    for (const e of queued) send(e.event, e.props);
  };
  document.head.appendChild(script);
}

/**
 * One product event. A no-op until analytics is initialised, never throws,
 * and only ever carries the context the caller names — never a form value,
 * and never a token.
 */
export function track(event: string, props?: EventProps): void {
  // Most of these clicks navigate at once, and a batched event would be lost
  // with the page: send them over sendBeacon, which survives the unload.
  send(event, props, true);
}

/** A route change is a navigation to the reader, even without a document load. */
function pageview(search: string): void {
  // ?ref= is how the landing page and badges attribute a visit; PostHog only
  // picks up utm_* on its own, so carry it explicitly.
  const ref = new URLSearchParams(search).get("ref");
  send("$pageview", ref ? { ref } : undefined);
}

/** Recording follows the route, the way a full page load used to decide it. */
function syncReplay(pathname: string): void {
  if (!client) return;
  try {
    if (replayPaths.test(pathname)) client.startSessionRecording?.();
    else client.stopSessionRecording?.();
  } catch {
    // An older loader without the controls records per its init flag.
  }
}

/** Mount once, inside the router: every route change is a pageview. */
export function usePageviews(): void {
  const { pathname, search } = useLocation();
  useEffect(() => {
    syncReplay(pathname);
    pageview(search);
  }, [pathname, search]);
}
