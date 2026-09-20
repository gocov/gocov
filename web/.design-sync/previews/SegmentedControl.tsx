import { useState } from "react";
import { SegmentedControl } from "gocov-web";

export function FileFilterWithCounts() {
  const [filter, setFilter] = useState("all");
  return (
    <SegmentedControl
      label="Filter files"
      value={filter}
      onChange={setFilter}
      options={[
        { value: "all", label: "All", count: 28 },
        { value: "changed", label: "Changed", count: 4 },
        { value: "source", label: "Source changed", count: 2 },
        { value: "coverage", label: "Coverage changed", disabled: true },
      ]}
    />
  );
}

export function ViewMode() {
  const [view, setView] = useState("tree");
  return (
    <SegmentedControl
      label="View mode"
      value={view}
      onChange={setView}
      options={[
        { value: "tree", label: "Tree" },
        { value: "list", label: "List" },
      ]}
    />
  );
}

export function LineFilter() {
  const [only, setOnly] = useState("miss");
  return (
    <SegmentedControl
      label="Line filter"
      value={only}
      onChange={setOnly}
      options={[
        { value: "all", label: "All lines", count: 184 },
        { value: "miss", label: "Uncovered only", count: 31 },
      ]}
    />
  );
}
