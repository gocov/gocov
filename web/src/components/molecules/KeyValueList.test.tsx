import { render, screen } from "@testing-library/react";
import { KeyValue, KeyValueList } from "./KeyValueList";

test("pairs a label with its value, and carries the layout", () => {
  const { container } = render(
    <KeyValueList layout="inline">
      <KeyValue label="Format" value="go" />
      <KeyValue label="Uploader" value="gocov-action" />
    </KeyValueList>,
  );
  expect(container.firstChild).toHaveClass("KeyValueList--inline");
  expect(screen.getByText("Format").tagName).toBe("DT");
  expect(screen.getByText("gocov-action").tagName).toBe("DD");
});
