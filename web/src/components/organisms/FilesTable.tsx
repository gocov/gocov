import { useDeferredValue, useMemo, useState, type CSSProperties, type ReactNode } from "react";
import { Link } from "react-router";
import { CoverageBar, Delta, Icon, Mono, TextInput } from "@/components/atoms";
import { BeforeAfter, Card, EmptyState, SectionHeader, SegmentedControl, Toolbar, UncoveredRanges } from "@/components/molecules";
import type { FileRow, FilesView } from "@/lib/api/types";
import { plural, splitPath } from "@/lib/format";
import { buildFileTree, defaultOpenDirs, filterTree, isChanged, visibleRows, type TreeNode } from "@/lib/tree";
import { routes } from "@/lib/urls";
import "./FilesTable.css";

type Mode = "tree" | "list";
type Filter = "all" | "changed" | "source" | "coverage";

const keeps: Record<Filter, (row: FileRow) => boolean> = {
  all: () => true,
  changed: isChanged,
  source: (row) => row.source_changed,
  coverage: (row) => row.coverage_changed,
};

const dash = <span className="muted small">&mdash;</span>;

/** A file's change against the baseline, or "new file" where there is none. */
function fileDelta(row: FileRow): ReactNode {
  if (row.new_file) return <span className="muted small">new file</span>;
  return <Delta value={row.before === null ? null : row.coverage - row.before} />;
}

function before(node: { before: number | null; coverage: number }, newFile: boolean): ReactNode {
  if (newFile) return <BeforeAfter before={null} after={node.coverage} />;
  if (node.before === null) return dash;
  return <BeforeAfter before={node.before} after={node.coverage} />;
}

const indent = (depth: number) => ({ paddingLeft: `calc(${depth} * var(--space-2))` }) as CSSProperties;

/**
 * The files behind a report: a directory tree by default, the flat list the
 * server sorted (changed first, then by delta) on demand. Search and the
 * change filter apply to both — a directory stays as long as anything under
 * it matches.
 */
export function FilesTable({ view, heading = "Files" }: { view: FilesView; heading?: string }) {
  const [mode, setMode] = useState<Mode>("tree");
  const [filter, setFilter] = useState<Filter>("all");
  const [query, setQuery] = useState("");
  // Only the directories a reader has toggled: everything else follows the
  // default, which recomputes when the report behind the table changes.
  const [toggled, setToggled] = useState<Record<string, boolean>>({});

  const tree = useMemo(() => buildFileTree(view.files), [view.files]);
  const defaults = useMemo(() => defaultOpenDirs(tree, view.has_base), [tree, view.has_base]);
  const isOpen = (path: string) => toggled[path] ?? defaults.has(path);
  const toggle = (path: string) => setToggled((was) => ({ ...was, [path]: !isOpen(path) }));

  // Filtering walks every file and rebuilds the tree, so it runs when the
  // search or the filter moves — not when a directory is opened or closed.
  // The search box updates at once; on a report with thousands of files
  // the filtering follows a keystroke behind rather than holding it up.
  const needle = useDeferredValue(query).trim().toLowerCase();
  const { matched, filtered } = useMemo(() => {
    const keep = (row: FileRow) => (needle === "" || row.path.toLowerCase().includes(needle)) && keeps[filter](row);
    return { matched: view.files.filter(keep), filtered: filterTree(tree, keep) };
  }, [view.files, tree, needle, filter]);
  const rows = mode === "list" ? matched.map(fileNode) : visibleRows(filtered, isOpen);

  const counts = useMemo(
    () => ({
      total: view.files.length,
      changed: view.files.filter(isChanged).length,
      source: view.files.filter((f) => f.source_changed).length,
      coverage: view.files.filter((f) => f.coverage_changed).length,
    }),
    [view.files],
  );

  return (
    <section className="FilesTable stack stack-1">
      <SectionHeader title={heading}>
        <SegmentedControl
          label="View mode"
          value={mode}
          onChange={(v) => setMode(v as Mode)}
          options={[
            { value: "tree", label: "Tree" },
            { value: "list", label: "List", count: counts.total },
          ]}
        />
        {view.has_base && (
          <SegmentedControl
            label="Filter files"
            value={filter}
            onChange={(v) => setFilter(v as Filter)}
            options={[
              { value: "all", label: "All", count: counts.total },
              { value: "changed", label: "Changed", count: counts.changed },
              { value: "source", label: "Source changed", count: counts.source },
              { value: "coverage", label: "Coverage changed", count: counts.coverage },
            ]}
          />
        )}
      </SectionHeader>

      <Toolbar
        label="File tools"
        left={
          <TextInput
            type="search"
            aria-label="Search files"
            placeholder="Search files&hellip;"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        }
        right={plural(matched.length, "file")}
      />

      <Card>
        {view.files.length === 0 ? (
          <Card.Body>
            <EmptyState message="No per-file data." />
          </Card.Body>
        ) : matched.length === 0 ? (
          <Card.Body>
            <EmptyState message="No files match the selected filter." />
          </Card.Body>
        ) : (
          <Card.Body flush>
            <table>
              <thead>
                <tr>
                  <th>File</th>
                  <th>Coverage</th>
                  {view.has_base ? (
                    <>
                      <th>
                        Before <Icon name="arrow-right" size={12} /> after
                      </th>
                      <th className="num">&Delta;</th>
                      <th className="hide-sm">Newly uncovered</th>
                    </>
                  ) : (
                    <>
                      <th className="num">Statements</th>
                      <th className="hide-sm">Uncovered lines</th>
                    </>
                  )}
                </tr>
              </thead>
              <tbody>
                {rows.map((node) => (
                  <tr key={`${node.kind}:${node.path}`}>
                    <td>
                      <span className="FilesTable__name" style={indent(mode === "tree" ? node.depth : 0)}>
                        {node.kind === "dir" ? (
                          <button
                            type="button"
                            className="FilesTable__toggle"
                            aria-expanded={isOpen(node.path)}
                            onClick={() => toggle(node.path)}
                          >
                            <Icon name={isOpen(node.path) ? "caret-down" : "caret-right"} size={12} />
                            <Icon name="folder" size={14} />
                            <Mono>{node.name}/</Mono>
                          </button>
                        ) : (
                          <Link className="FilesTable__file" to={routes.source(node.row.upload_id, node.path)}>
                            {mode === "tree" && <Icon name="file" size={14} />}
                            <Mono>
                              {mode === "tree" ? (
                                node.name
                              ) : (
                                <>
                                  <span className="FilesTable__dir">{splitPath(node.path)[0]}</span>
                                  {splitPath(node.path)[1]}
                                </>
                              )}
                            </Mono>
                          </Link>
                        )}
                      </span>
                    </td>
                    <td>
                      <CoverageBar value={node.coverage} />
                    </td>
                    {view.has_base ? (
                      <>
                        <td>{before(node, node.kind === "file" && node.row.new_file)}</td>
                        <td className="num">
                          {node.kind === "file" ? (
                            fileDelta(node.row)
                          ) : (
                            <Delta value={node.before === null ? null : node.coverage - node.before} />
                          )}
                        </td>
                        <td className="hide-sm">
                          {node.kind === "file" ? <UncoveredRanges ranges={node.row.newly_uncovered} /> : dash}
                        </td>
                      </>
                    ) : (
                      <>
                        <td className="num muted small">
                          {node.coveredStmts}/{node.totalStmts}
                        </td>
                        <td className="hide-sm">
                          {node.kind === "file" ? <UncoveredRanges ranges={node.row.uncovered} /> : dash}
                        </td>
                      </>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </Card.Body>
        )}
      </Card>
    </section>
  );
}

/** A flat-list row, shaped like a tree row so one table renders both. */
function fileNode(row: FileRow): TreeNode {
  return {
    kind: "file",
    path: row.path,
    name: splitPath(row.path)[1],
    depth: 0,
    coveredStmts: row.covered_stmts,
    totalStmts: row.total_stmts,
    coverage: row.coverage,
    before: row.before,
    changed: isChanged(row),
    sourceChanged: row.source_changed,
    coverageChanged: row.coverage_changed,
    row,
  };
}
