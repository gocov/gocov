// Browser storage as a convenience, never a dependency: it can be denied
// outright (private mode, blocked site data), so every access is wrapped
// and every caller renders correctly on the fallback. Nothing here may
// decide what the server already knows.

type Store = "localStorage" | "sessionStorage";

/** The stored value, or null when there is none or storage is denied. */
export function readStored(store: Store, key: string): string | null {
  try {
    return window[store].getItem(key);
  } catch {
    return null;
  }
}

/** Best effort: a value that cannot be kept is simply forgotten. */
export function writeStored(store: Store, key: string, value: string): void {
  try {
    window[store].setItem(key, value);
  } catch {
    // Nothing depends on it sticking.
  }
}
