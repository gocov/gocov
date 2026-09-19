import { render, screen } from "@testing-library/react";
import { ForgeMark } from "./ForgeMark";

test("names the forge when it is the only thing on screen", () => {
  render(<ForgeMark forge="github" label="GitHub" />);
  expect(screen.getByRole("img", { name: "GitHub" })).toBeInTheDocument();
});

test("GitLab is drawn optically larger, unknown forges draw nothing", () => {
  const { container: gl } = render(<ForgeMark forge="gitlab" size={20} />);
  expect(gl.querySelector("svg")).toHaveAttribute("width", "24");
  const { container: none } = render(<ForgeMark forge="gitea" />);
  expect(none).toBeEmptyDOMElement();
});
