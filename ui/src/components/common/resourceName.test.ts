import { describe, expect, it } from "vitest";
import { isValidNamespace, isValidObjectName, sanitizeName } from "./resourceName";

describe("resource name rules", () => {
  it("accepts dotted object names up to 253 characters, as the API does", () => {
    expect(isValidObjectName("team.reviewer")).toBe(true);
    expect(isValidObjectName("a".repeat(253))).toBe(true);
    expect(isValidObjectName("a".repeat(254))).toBe(false);
    expect(isValidObjectName("team..reviewer")).toBe(false);
    expect(isValidObjectName(".reviewer")).toBe(false);
    expect(isValidObjectName("Reviewer")).toBe(false);
  });

  it("keeps namespaces to a DNS-1123 label", () => {
    expect(isValidNamespace("team-a")).toBe(true);
    expect(isValidNamespace("team.a")).toBe(false);
    expect(isValidNamespace("a".repeat(64))).toBe(false);
  });

  it("rewrites text to a name's characters, keeping dots only for object names", () => {
    expect(sanitizeName("  My Team_Agent v2.1", true)).toBe("my-teamagent-v2.1");
    expect(sanitizeName("Team.A", false)).toBe("teama");
  });
});
