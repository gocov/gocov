import { readStored, writeStored } from "./storage";

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
});

afterEach(() => vi.restoreAllMocks());

test("keeps a value in the store it was given", () => {
  writeStored("sessionStorage", "k", "v");
  expect(readStored("sessionStorage", "k")).toBe("v");
  expect(readStored("localStorage", "k")).toBeNull();
});

test("denied storage reads as nothing and writes as a no-op", () => {
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
    throw new Error("denied");
  });
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("denied");
  });
  expect(readStored("localStorage", "k")).toBeNull();
  expect(() => writeStored("localStorage", "k", "v")).not.toThrow();
});
