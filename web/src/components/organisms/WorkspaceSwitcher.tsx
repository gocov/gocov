import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { ForgeMark, Icon, Mono, TextInput } from "@/components/atoms";
import type { WorkspaceGroup } from "@/lib/api/types";
import { forgeLabel, pct } from "@/lib/format";
import { routes } from "@/lib/urls";
import "./WorkspaceSwitcher.css";

/** Above this many workspaces the popover is worth searching. */
const SEARCHABLE = 7;

const wsParam = (group: WorkspaceGroup) => `${group.forge}/${group.prefix}`;

/**
 * The dashboard's title, and the way between workspaces: the current one
 * reads as the page's name, and the caret opens the rest. A viewer with a
 * single workspace gets the title alone — there is nowhere to switch to.
 */
export function WorkspaceSwitcher({
  current,
  groups,
  canOnboard = false,
}: {
  current: WorkspaceGroup;
  groups: WorkspaceGroup[];
  canOnboard?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const root = useRef<HTMLSpanElement>(null);

  useEffect(() => {
    if (!open) return;
    const onPointer = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const name = (
    <>
      <Mono className="WorkspaceSwitcher__name">{current.prefix}</Mono>
      <span className="WorkspaceSwitcher__forge">
        <ForgeMark forge={current.forge} size={14} />
        {forgeLabel(current.forge)}
      </span>
    </>
  );

  if (groups.length < 2) return <span className="WorkspaceSwitcher">{name}</span>;

  const q = query.trim().toLowerCase();
  const shown = q === "" ? groups : groups.filter((g) => g.prefix.toLowerCase().includes(q));

  return (
    <span className="WorkspaceSwitcher" ref={root}>
      <button
        type="button"
        className="WorkspaceSwitcher__trigger"
        aria-expanded={open}
        aria-haspopup="true"
        onClick={() => setOpen((was) => !was)}
      >
        {name}
        <Icon name="caret-down" size={12} />
      </button>
      {open && (
        <span className="WorkspaceSwitcher__pop" aria-label="Workspaces">
          {groups.length > SEARCHABLE && (
            <span className="WorkspaceSwitcher__search">
              <TextInput
                type="search"
                autoFocus
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Find a workspace"
                aria-label="Find a workspace"
              />
            </span>
          )}
          <span className="WorkspaceSwitcher__list">
            {shown.map((group) => (
              <Link
                key={wsParam(group)}
                to={routes.dashboard(wsParam(group))}
                className="WorkspaceSwitcher__item"
                aria-current={group.current ? "true" : undefined}
                onClick={() => setOpen(false)}
              >
                <ForgeMark forge={group.forge} size={14} />
                <Mono className="WorkspaceSwitcher__itemName">{group.prefix}</Mono>
                <span className="WorkspaceSwitcher__pct">{group.coverage === null ? "—" : pct(group.coverage)}</span>
              </Link>
            ))}
          </span>
          {canOnboard && (
            <span className="WorkspaceSwitcher__foot">
              <Link to={routes.onboarding()} onClick={() => setOpen(false)}>Connect a workspace</Link>
            </span>
          )}
        </span>
      )}
    </span>
  );
}
