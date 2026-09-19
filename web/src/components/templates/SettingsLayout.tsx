import { useEffect, useRef, useState, type ReactNode } from "react";
import "./SettingsLayout.css";

export interface SettingsNavItem {
  /** The id of the section the link jumps to. */
  id: string;
  label: string;
  /** Sections carrying the same group are listed under its name. */
  group?: string;
}

/** One card of the page, and the anchor the sidebar points at. */
function SettingsSection({ id, children }: { id: string; children: ReactNode }) {
  return (
    <section className="SettingsLayout__section" id={id}>
      {children}
    </section>
  );
}

/** The nav, in the order given, cut into the runs that share a group. */
function grouped(nav: SettingsNavItem[]): { group: string; items: SettingsNavItem[] }[] {
  const out: { group: string; items: SettingsNavItem[] }[] = [];
  for (const item of nav) {
    const group = item.group ?? "";
    const last = out[out.length - 1];
    if (last !== undefined && last.group === group) last.items.push(item);
    else out.push({ group, items: [item] });
  }
  return out;
}

function SettingsLayoutRoot({ header, nav, children }: { header: ReactNode; nav: SettingsNavItem[]; children: ReactNode }) {
  const [active, setActive] = useState(() => nav[0]?.id ?? "");
  // A click wins over what is merely scrolling past for a moment, so the
  // jump does not light up every section it travels through.
  const pinnedUntil = useRef(0);
  const ids = nav.map((item) => item.id).join(" ");

  useEffect(() => {
    // jsdom has no IntersectionObserver; the last clicked link is then the
    // only highlight, which is the behaviour without JavaScript too.
    if (typeof IntersectionObserver !== "function") return;
    const tops = new Map<string, number>();
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          tops.set(entry.target.id, entry.isIntersecting ? entry.boundingClientRect.top : Number.POSITIVE_INFINITY);
        }
        if (Date.now() < pinnedUntil.current) return;
        let best = "";
        let bestTop = Number.POSITIVE_INFINITY;
        for (const [id, top] of tops) {
          if (top < bestTop) {
            best = id;
            bestTop = top;
          }
        }
        if (best !== "") setActive(best);
      },
      { rootMargin: "0px 0px -60% 0px" },
    );
    for (const id of ids.split(" ")) {
      const el = document.getElementById(id);
      if (el !== null) observer.observe(el);
    }
    return () => observer.disconnect();
  }, [ids]);

  return (
    <div className="SettingsLayout">
      <div className="SettingsLayout__header">{header}</div>
      <nav className="SettingsLayout__nav" aria-label="Settings sections">
        {grouped(nav).map(({ group, items }) => (
          <div className="SettingsLayout__group" key={group}>
            {group !== "" && <span className="SettingsLayout__groupName">{group}</span>}
            {items.map((item) => (
              <a
                key={item.id}
                className="SettingsLayout__link"
                href={`#${item.id}`}
                aria-current={active === item.id ? "true" : undefined}
                onClick={() => {
                  pinnedUntil.current = Date.now() + 700;
                  setActive(item.id);
                }}
              >
                {item.label}
              </a>
            ))}
          </div>
        ))}
      </nav>
      <div className="SettingsLayout__main">{children}</div>
    </div>
  );
}

/**
 * The frame both settings pages sit in: the header across the top, a
 * sticky list of the sections down the side, and the cards themselves in
 * one column. Wrap each card in `SettingsLayout.Section` so its id matches
 * the nav item that points at it.
 */
export const SettingsLayout = Object.assign(SettingsLayoutRoot, { Section: SettingsSection });
