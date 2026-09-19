import { render, screen } from "@testing-library/react";
import { Card } from "./Card";

test("titles, bodies and footers stack inside one surface", () => {
  const { container } = render(
    <Card>
      <Card.Header title="Uploads" actions={<span>3</span>} />
      <Card.Body>Every repository uploads with this token.</Card.Body>
      <Card.Footer>Rotating takes effect immediately.</Card.Footer>
    </Card>,
  );
  expect(screen.getByRole("heading", { level: 2, name: "Uploads" })).toBeInTheDocument();
  expect(screen.getByText("Every repository uploads with this token.")).toBeInTheDocument();
  expect(container.firstChild).toHaveClass("Card");
  expect(container.firstChild).not.toHaveClass("Card--danger");
});

test("the danger variant is a colour, not a new shape", () => {
  const { container } = render(
    <Card danger>
      <Card.Header title="Delete workspace" />
    </Card>,
  );
  expect(container.firstChild).toHaveClass("Card--danger");
});
