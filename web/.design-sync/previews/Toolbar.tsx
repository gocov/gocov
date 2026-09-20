import { useState } from "react";
import { Button, SegmentedControl, Select, TextInput, Toolbar } from "gocov-web";

export function SearchLeftCountRight() {
  const [query, setQuery] = useState("internal/server");
  return (
    <Toolbar
      label="File tools"
      left={
        <TextInput
          type="search"
          aria-label="Search files"
          placeholder="Search files…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      }
      right="28 files"
    />
  );
}

export function RepositoryFilters() {
  const [filter, setFilter] = useState("all");
  return (
    <Toolbar
      label="Repository filters"
      left={
        <SegmentedControl
          label="Filter repositories"
          value={filter}
          onChange={setFilter}
          options={[
            { value: "all", label: "All", count: 12 },
            { value: "failing", label: "Failing gates", count: 2 },
            { value: "stale", label: "Stale", count: 1 },
          ]}
        />
      }
      right={
        <Select aria-label="Sort repositories" defaultValue="cov">
          <option value="cov">Lowest coverage</option>
          <option value="name">Name</option>
          <option value="recent">Most recent upload</option>
        </Select>
      }
    />
  );
}

export function SourceTools() {
  const [only, setOnly] = useState("all");
  return (
    <Toolbar
      label="Source tools"
      left={
        <>
          <SegmentedControl
            label="Line filter"
            value={only}
            onChange={setOnly}
            options={[
              { value: "all", label: "All lines" },
              { value: "miss", label: "Uncovered only" },
            ]}
          />
          <Button size="sm" icon="arrow-up">
            Previous miss
          </Button>
          <Button size="sm" icon="arrow-down">
            Next miss
          </Button>
        </>
      }
      right="31 uncovered lines"
    />
  );
}
