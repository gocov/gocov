import { useState } from "react";
import { Link } from "react-router";
import { Chip, CoverageBar, Delta, Mono, Select, Sparkline, TextInput } from "@/components/atoms";
import { Card, EmptyState, SegmentedControl, Toolbar } from "@/components/molecules";
import type { DashRepo } from "@/lib/api/types";
import { repoCounts, visibleRepos, type RepoFilter, type RepoSort } from "@/lib/dashboard";
import { timeAgo } from "@/lib/format";
import { routes } from "@/lib/urls";
import "./ReposTable.css";

const filterLabels: { value: RepoFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "failing", label: "Failing" },
  { value: "stale", label: "Stale" },
  { value: "nogate", label: "No gate" },
];

const sortLabels: { value: RepoSort; label: string }[] = [
  { value: "cov", label: "Sort: lowest coverage" },
  { value: "drop", label: "Sort: biggest drop" },
  { value: "recent", label: "Sort: recently uploaded" },
  { value: "name", label: "Sort: name" },
];

const isSort = (v: string): v is RepoSort => sortLabels.some((s) => s.value === v);

/** The gate cell: a state, or the way to set one. */
function GateCell({ repo }: { repo: DashRepo }) {
  return (
    <span className="ReposTable__gate">
      {repo.gate === "pass" && <Chip tone="good">Passing</Chip>}
      {repo.gate === "fail" && <Chip tone="bad">Failing</Chip>}
      {repo.gate === "none" && (
        <Link className="ReposTable__setGate" to={routes.repoSettings(repo.forge, repo.slug)}>
          Set a gate
        </Link>
      )}
      {repo.stale && <Chip tone="warn">Stale</Chip>}
    </span>
  );
}

/**
 * Every repository in the workspace, with the filter, search and sort that
 * slice it. All three work on the rows already in hand, so the table answers
 * without a round-trip.
 */
export function ReposTable({ repos }: { repos: DashRepo[] }) {
  const [filter, setFilter] = useState<RepoFilter>("all");
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<RepoSort>("cov");

  const counts = repoCounts(repos);
  const rows = visibleRepos(repos, { filter, search, sort });

  return (
    <div className="ReposTable">
      <Toolbar
        label="Repository filters"
        left={
          <>
            <SegmentedControl
              label="Filter repositories"
              value={filter}
              onChange={(v) => setFilter(v as RepoFilter)}
              options={filterLabels.map(({ value, label }) => ({ value, label, count: counts[value] }))}
            />
            <span className="ReposTable__search">
              <TextInput
                type="search"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search repositories…"
                aria-label="Search repositories"
              />
            </span>
          </>
        }
        right={
          <span className="ReposTable__sort">
            <Select aria-label="Sort by" value={sort} onChange={(e) => isSort(e.target.value) && setSort(e.target.value)}>
              {sortLabels.map(({ value, label }) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </Select>
          </span>
        }
      />
      <Card>
        <Card.Body flush>
          {rows.length === 0 ? (
            <EmptyState message="No repositories match." />
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Repository</th>
                  <th>Coverage</th>
                  <th className="num">Δ</th>
                  <th className="hide-sm">30 days</th>
                  <th>Gate</th>
                  <th className="hide-sm">Last upload</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((repo) => (
                  <tr key={`${repo.forge}/${repo.slug}`}>
                    <td>
                      <Link className="ReposTable__repo" to={routes.repo(repo.forge, repo.slug)}>
                        <Mono>{repo.name}</Mono>
                      </Link>
                    </td>
                    <td>{repo.coverage === null ? <span className="muted">—</span> : <CoverageBar value={repo.coverage} />}</td>
                    <td className="num">
                      <Delta value={repo.delta} />
                    </td>
                    <td className="hide-sm">
                      <Sparkline series={repo.series} stale={repo.stale} />
                    </td>
                    <td>
                      <GateCell repo={repo} />
                    </td>
                    <td className="hide-sm muted small">{repo.uploaded_at === null ? "never" : timeAgo(repo.uploaded_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card.Body>
      </Card>
    </div>
  );
}
