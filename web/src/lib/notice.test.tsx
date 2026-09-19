import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router";
import { useUrlNotice } from "./notice";

const at = (path: string, codes?: Record<string, string>) => {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <MemoryRouter initialEntries={[path]}>{children}</MemoryRouter>
  );
  return renderHook(() => ({ notice: useUrlNotice(codes ? { codes } : undefined), location: useLocation() }), {
    wrapper,
  });
};

const codes = { connect_failed: "Connecting to the forge did not complete." };

test("no parameter, no message", () => {
  const { result } = at("/?ws=github/acme");
  expect(result.current.notice).toBeNull();
  expect(result.current.location.search).toBe("?ws=github/acme");
});

test("a notice reads neutral and leaves the URL once it has been read", async () => {
  const { result } = at("/workspaces/github/acme?notice=GitHub+App+connected.&ws=github%2Facme");
  expect(result.current.notice).toEqual({ text: "GitHub App connected.", tone: "neutral" });
  await waitFor(() => expect(result.current.location.search).toBe("?ws=github%2Facme"));
  // The message stays on screen after the URL has been cleaned up.
  expect(result.current.notice).toEqual({ text: "GitHub App connected.", tone: "neutral" });
});

test("an error reads bad", async () => {
  const { result } = at("/?error=The+install+could+not+be+matched+to+a+workspace.");
  expect(result.current.notice).toEqual({
    text: "The install could not be matched to a workspace.",
    tone: "bad",
  });
  await waitFor(() => expect(result.current.location.search).toBe(""));
});

test("an overlong message is cut to a sentence's worth", () => {
  const { result } = at("/?notice=" + "x".repeat(500));
  expect(result.current.notice?.text).toHaveLength(300);
});

test("an empty value is not a message", () => {
  const { result } = at("/?notice=");
  expect(result.current.notice).toBeNull();
});

test("a code the page knows becomes its sentence, not the code", async () => {
  const { result } = at("/workspaces/github/acme?error=connect_failed", codes);
  expect(result.current.notice).toEqual({
    text: "Connecting to the forge did not complete.",
    tone: "bad",
  });
  await waitFor(() => expect(result.current.location.search).toBe(""));
});

test("a code the page does not know is still shown as text", () => {
  const { result } = at("/?error=something_else", codes);
  expect(result.current.notice).toEqual({ text: "something_else", tone: "bad" });
});

test("without a table a code reads as the raw text it is", () => {
  const { result } = at("/?error=connect_failed");
  expect(result.current.notice?.text).toBe("connect_failed");
});
