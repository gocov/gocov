import { render, screen } from "@testing-library/react";
import { StatRow, StatTile } from "./StatRow";

test("lays out as many columns as it has tiles", () => {
  const { container } = render(
    <StatRow>
      <StatTile label="Coverage" value="74.0%" hint="statement-weighted" />
      <StatTile label="Gates" value="6/8" />
      <StatTile label="Stale" value="1" />
    </StatRow>,
  );
  expect(container.querySelector(".StatRow")).toHaveAttribute("style", expect.stringContaining("--stat-cols: 3"));
  expect(screen.getByText("statement-weighted")).toBeInTheDocument();
  expect(screen.getByText("6/8")).toBeInTheDocument();
});
