import { FilesTable } from "gocov-web";

const file = (path: string, over: Record<string, unknown> = {}) => ({
  path,
  coverage: 70,
  covered_stmts: 7,
  total_stmts: 10,
  uncovered: "12-18, 40",
  before: null,
  before_covered_stmts: null,
  before_total_stmts: null,
  new_file: false,
  newly_uncovered: "",
  source_changed: false,
  coverage_changed: false,
  ...over,
});

export function AgainstABaseline() {
  return (
    <FilesTable
      heading="Files on main"
      view={{
        upload_id: 412,
        has_base: true,
        files: [
          file("internal/server/upload.go", {
            coverage: 80,
            covered_stmts: 40,
            total_stmts: 50,
            before: 62,
            before_covered_stmts: 31,
            before_total_stmts: 50,
            source_changed: true,
            coverage_changed: true,
            newly_uncovered: "44-46",
            uncovered: "44-46, 88, 91",
          }),
          file("internal/server/spa.go", {
            coverage: 80,
            covered_stmts: 8,
            total_stmts: 10,
            before: 80,
            before_covered_stmts: 8,
            before_total_stmts: 10,
          }),
          file("internal/core/pipeline.go", { new_file: true, coverage: 55, covered_stmts: 11, total_stmts: 20 }),
          file("main.go", {
            coverage: 90,
            covered_stmts: 9,
            total_stmts: 10,
            before: 90,
            before_covered_stmts: 9,
            before_total_stmts: 10,
          }),
        ],
      }}
    />
  );
}

export function WithNoBaseline() {
  return (
    <FilesTable
      view={{
        upload_id: 7,
        has_base: false,
        files: [
          file("cmd/gocov/main.go", { coverage: 37.5, covered_stmts: 3, total_stmts: 8, uncovered: "12-18, 40, 55-57" }),
          file("internal/profile/detect.go", { coverage: 96, covered_stmts: 24, total_stmts: 25, uncovered: "61" }),
          file("doc.go", { coverage: 70, covered_stmts: 7, total_stmts: 10 }),
        ],
      }}
    />
  );
}

export function NoPerFileData() {
  return <FilesTable heading="Files" view={{ upload_id: 9, has_base: false, files: [] }} />;
}
