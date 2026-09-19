// A server redirect that lands on an app route can carry a one-shot message
// in the query (?error=connect_failed, ?connect=owners_only&ws=…). It is
// read once and taken back out of the URL, so a reload or a shared link does
// not replay it — while what was read stays for as long as the page is open.

import { useEffect, useState } from "react";
import { useSearchParams } from "react-router";

interface Options {
  /** The message is there when any of these is; they are stripped too. */
  when: string[];
  /** Context that travels with it, stripped along with it and only then. */
  also?: string[];
}

export function useOneShotParams<T>({ when, also = [] }: Options, read: (params: URLSearchParams) => T): T {
  const [params, setParams] = useSearchParams();

  // Captured on the first render: the effect below removes the parameters,
  // and what they said has to survive that.
  const [value] = useState(() => read(params));

  useEffect(() => {
    if (!when.some((key) => params.has(key))) return;
    const next = new URLSearchParams(params);
    for (const key of [...when, ...also]) next.delete(key);
    setParams(next, { replace: true });
    // `when` and `also` are literals at every call site, so they are not
    // dependencies: the query string is what this follows.
  }, [params, setParams]);

  return value;
}
