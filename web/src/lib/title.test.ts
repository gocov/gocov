import { renderHook } from "@testing-library/react";
import { pageTitle, usePageTitle } from "./title";

test("a title is suffixed with the product name", () => {
  expect(pageTitle("repositories")).toBe("repositories — gocov");
});

test.each([undefined, ""])("nothing to say leaves the bare product name (%s)", (title) => {
  expect(pageTitle(title)).toBe("gocov");
});

test("the hook writes the document title, and rewrites it when the title arrives", () => {
  document.title = "stale";
  const { rerender } = renderHook(({ t }: { t: string | undefined }) => usePageTitle(t), {
    initialProps: { t: undefined as string | undefined },
  });
  expect(document.title).toBe("gocov");

  rerender({ t: "acme/api code coverage" });
  expect(document.title).toBe("acme/api code coverage — gocov");
});

test("unmounting restores nothing: the next page sets its own title", () => {
  const { unmount } = renderHook(() => usePageTitle("sign in"));
  expect(document.title).toBe("sign in — gocov");
  unmount();
  expect(document.title).toBe("sign in — gocov");
});
