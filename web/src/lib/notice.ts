// One-shot messages the server hands back in the URL. After a GitHub App
// install or a Bitbucket/GitLab grant the browser lands on an app route with
// ?notice= (it worked) or ?error= (it did not). The value is a code, never
// prose (?error=connect_failed): the page that receives it supplies the
// sentence, shows it once and takes the parameter back out of the URL, so a
// reload or a share does not repeat it.
//
// Anyone can type a query string, so a value the page has no sentence for is
// dropped rather than shown: the banner only ever says what the app wrote.

import { useOneShotParams } from "./oneShotParams";

export interface UrlNotice {
  text: string;
  tone: "neutral" | "bad";
}

export interface UrlNoticeOptions {
  /**
   * Sentences for the codes the server sends (`connect_failed`), keyed by the
   * code. What each one says depends on the page it lands on, so the page
   * supplies the table.
   */
  codes: Record<string, string>;
}

/**
 * Reads ?notice= / ?error= once, then strips both from the URL. Only a value
 * the `codes` table knows becomes a message; anything else is not the
 * server's and is ignored.
 */
export function useUrlNotice({ codes }: UrlNoticeOptions): UrlNotice | null {
  return useOneShotParams({ when: ["notice", "error"] }, (params) => {
    // hasOwn: "constructor" is a key of every object, and not a sentence.
    const say = (value: string | null) => (value !== null && Object.hasOwn(codes, value) ? codes[value] : undefined);
    const good = say(params.get("notice"));
    if (good) return { text: good, tone: "neutral" };
    const bad = say(params.get("error"));
    if (bad) return { text: bad, tone: "bad" };
    return null;
  });
}
