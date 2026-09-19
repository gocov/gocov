// The document title, which used to come from the Go template. The server
// still injects one into the shell it serves, so the first paint is right for
// a bookmark or a share; from then on every route sets its own.

import { useEffect } from "react";

/** How a page reads in a tab, history entry or bookmark. */
export function pageTitle(title: string | undefined): string {
  return title ? `${title} — gocov` : "gocov";
}

/**
 * Sets the document title for as long as this page is the one on screen.
 * A page whose title needs loaded data passes undefined until it has it, and
 * the tab reads the bare product name in the meantime. Nothing is restored on
 * unmount: the page that comes next sets its own.
 */
export function usePageTitle(title: string | undefined): void {
  useEffect(() => {
    document.title = pageTitle(title);
  }, [title]);
}
