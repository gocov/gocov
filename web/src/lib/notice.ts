// One-shot messages the server hands back in the URL. After a GitHub App
// install or a Bitbucket/GitLab grant the browser lands on an app route with
// ?notice= (it worked) or ?error= (it did not). Newer redirects carry a code
// rather than prose (?error=connect_failed) and the page that receives it
// supplies the sentence; the app shows the message once and takes the
// parameter back out of the URL, so a reload or a share does not repeat it.

import { useEffect, useState } from "react";
import { useSearchParams } from "react-router";

/** Anything longer is a URL someone else built; a sentence is the contract. */
const MAX = 300;

export interface UrlNotice {
  text: string;
  tone: "neutral" | "bad";
}

export interface UrlNoticeOptions {
  /**
   * Sentences for the codes the server sends instead of prose (`connect_failed`),
   * keyed by the code. What each one says depends on the page it lands on, so
   * the page supplies the table.
   */
  codes?: Record<string, string>;
}

/**
 * Reads ?notice= / ?error= once, then strips both from the URL. A value the
 * `codes` table knows is a code and becomes that sentence; anything else is
 * whatever the server put there, so it is rendered as text and never as
 * markup, and it is capped at 300 characters.
 */
export function useUrlNotice({ codes }: UrlNoticeOptions = {}): UrlNotice | null {
  const [params, setParams] = useSearchParams();

  // Captured on the first render: the effect below removes the parameters,
  // and the message has to survive that.
  const [notice] = useState<UrlNotice | null>(() => {
    const say = (value: string) => codes?.[value] ?? value.slice(0, MAX);
    const good = params.get("notice");
    if (good) return { text: say(good), tone: "neutral" };
    const bad = params.get("error");
    if (bad) return { text: say(bad), tone: "bad" };
    return null;
  });

  useEffect(() => {
    if (!params.has("notice") && !params.has("error")) return;
    const next = new URLSearchParams(params);
    next.delete("notice");
    next.delete("error");
    setParams(next, { replace: true });
  }, [params, setParams]);

  return notice;
}
