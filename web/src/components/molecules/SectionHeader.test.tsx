import { render, screen } from "@testing-library/react";
import { SectionHeader } from "./SectionHeader";

test("is a second-level heading with an aside", () => {
  render(<SectionHeader title="Files">28 files</SectionHeader>);
  expect(screen.getByRole("heading", { level: 2, name: "Files" })).toBeInTheDocument();
  expect(screen.getByText("28 files")).toBeInTheDocument();
});
