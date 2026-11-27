import { describe, expect, it } from "vitest";
import { copyName, copyPath, copySource } from "./copyFrom";

describe("copyPath", () => {
  it("encodes the source as namespace/name", () => {
    expect(copyPath("/agents/new", { namespace: "ns", name: "web" })).toBe("/agents/new?from=ns%2Fweb");
  });
});

describe("copyName", () => {
  it("appends -copy, within 63 characters", () => {
    expect(copyName("web")).toBe("web-copy");
    expect(copyName("a".repeat(63))).toBe(`${"a".repeat(58)}-copy`);
  });

  it("does not leave a trailing `.` or `-` before the suffix", () => {
    expect(copyName(`${"a".repeat(57)}.${"b".repeat(5)}`)).toBe(`${"a".repeat(57)}-copy`);
    expect(copyName(`${"a".repeat(57)}-${"b".repeat(5)}`)).toBe(`${"a".repeat(57)}-copy`);
  });
});

describe("copySource", () => {
  it.each([
    ["ns/web", { namespace: "ns", name: "web" }],
    ["web", undefined],
    ["ns/", undefined],
    ["/web", undefined],
    ["a/b/c", undefined],
    ["", undefined],
  ])("reads %j", (from, want) => {
    expect(copySource(new URLSearchParams({ from }))).toEqual(want);
  });
  it("is no source when absent", () => {
    expect(copySource(new URLSearchParams())).toBeUndefined();
  });
});
