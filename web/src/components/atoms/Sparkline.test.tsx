import { render, screen } from "@testing-library/react";
import { Sparkline } from "./Sparkline";

test("a single point cannot be a line", () => {
  const { container } = render(<Sparkline series={[70]} />);
  expect(container.textContent).toBe("—");
});

test("plots the series across the box and names its direction", () => {
  const { container } = render(<Sparkline series={[60, 70, 80]} />);
  expect(screen.getByRole("img")).toHaveAccessibleName("Coverage trend: up");
  // lowest point at the bottom of the band, highest at the top, evenly spaced.
  expect(container.querySelector("path")).toHaveAttribute("d", "M0 19 L38 11 L76 3");
});

test("a constant series is a flat mid line", () => {
  const { container } = render(<Sparkline series={[50, 50, 50]} />);
  expect(container.querySelector("path")).toHaveAttribute("d", "M0 11 L38 11 L76 11");
  expect(screen.getByRole("img")).toHaveAccessibleName("Coverage trend: flat");
});

test("a stale series stops early and continues as a dashed tail", () => {
  const { container } = render(<Sparkline series={[80, 60]} stale />);
  const paths = container.querySelectorAll("path");
  expect(paths).toHaveLength(2);
  expect(paths[0]).toHaveAttribute("d", "M0 3 L46 19");
  expect(paths[1]).toHaveAttribute("d", "M46 19 L76 19");
  expect(screen.getByRole("img")).toHaveAccessibleName("Coverage trend: stopped");
});
