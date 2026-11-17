import { describe, expect, it } from "vitest";
import { safeDecode } from "./toolAnchor";

describe("safeDecode", () => {
  it("decodes percent-encoding", () => {
    expect(safeDecode("tool-a%20b")).toBe("tool-a b");
  });

  it("returns a malformed hash unchanged", () => {
    expect(safeDecode("tool-%E0")).toBe("tool-%E0");
  });
});
