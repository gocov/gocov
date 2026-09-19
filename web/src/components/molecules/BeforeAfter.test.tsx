import { render } from "@testing-library/react";
import { BeforeAfter } from "./BeforeAfter";

test("reads as one move from one figure to the other", () => {
  const { container } = render(<BeforeAfter before={71.2} after={74} />);
  expect(container.textContent).toBe("71.2%74.0%");
});

test("a new file has nothing on the left", () => {
  const { container } = render(<BeforeAfter before={null} after={62.5} />);
  expect(container.querySelector(".BeforeAfter__before")).toHaveTextContent("—");
});
