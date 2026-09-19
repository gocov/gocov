import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router";
import { useUrlNotice } from "./notice";

const codes = {
  connect_failed: "Connecting to the forge did not complete.",
  connected: "Workspace connected.",
};

const at = (path: string) => {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <MemoryRouter initialEntries={[path]}>{children}</MemoryRouter>
  );
  return renderHook(() => ({ notice: useUrlNotice({ codes }), location: useLocation() }), { wrapper });
};

test("no parameter, no message", () => {
  const { result } = at("/?ws=github/acme");
  expect(result.current.notice).toBeNull();
  expect(result.current.location.search).toBe("?ws=github/acme");
});

test("a notice reads neutral and leaves the URL once it has been read", async () => {
  const { result } = at("/workspaces/github/acme?notice=connected&ws=github%2Facme");
  expect(result.current.notice).toEqual({ text: "Workspace connected.", tone: "neutral" });
  await waitFor(() => expect(result.current.location.search).toBe("?ws=github%2Facme"));
  // The message stays on screen after the URL has been cleaned up.
  expect(result.current.notice).toEqual({ text: "Workspace connected.", tone: "neutral" });
});

test("an error code becomes the page's sentence and reads bad", async () => {
  const { result } = at("/workspaces/github/acme?error=connect_failed");
  expect(result.current.notice).toEqual({
    text: "Connecting to the forge did not complete.",
    tone: "bad",
  });
  await waitFor(() => expect(result.current.location.search).toBe(""));
});

test("an empty value is not a message", () => {
  const { result } = at("/?notice=");
  expect(result.current.notice).toBeNull();
});

test("text the page has no sentence for is never shown, and still leaves the URL", async () => {
  const { result } = at("/?error=Your+session+expired.+Sign+in+at+evil.example");
  expect(result.current.notice).toBeNull();
  await waitFor(() => expect(result.current.location.search).toBe(""));
});

test("a key every object has is not a code", () => {
  const { result } = at("/?error=constructor");
  expect(result.current.notice).toBeNull();
});
