import { describe, expect, it } from "vitest";
import type { Header } from "@/api";
import { credentialHeaderIssues } from "./credentialHeaders";

function actorHeader(audiences = ["https://service.example"], expirationSeconds?: number): Header {
  return { name: "Authorization", prefix: "Bearer ", valueFrom: { actorToken: { audiences, expirationSeconds } } };
}

describe("actor credential headers", () => {
  it.each([undefined, 300, 900, 3600])("accepts lifetime %s", (lifetime) => {
    expect(credentialHeaderIssues([actorHeader(undefined, lifetime)])).toEqual([]);
  });

  it.each([0, 299, 3601, 900.5, NaN, Infinity])("rejects lifetime %s", (lifetime) => {
    expect(credentialHeaderIssues([actorHeader(undefined, lifetime)])).toContain(
      "Actor token lifetime must be a whole number between 300 and 3600 seconds.",
    );
  });

  it("requires between one and sixteen audiences", () => {
    const audiences = Array.from({ length: 16 }, (_, index) => `service-${index}`);
    expect(credentialHeaderIssues([actorHeader(audiences)])).toEqual([]);
    for (const invalid of [[], [...audiences, "another-service"]]) {
      expect(credentialHeaderIssues([actorHeader(invalid)])).toContain("Actor tokens need between 1 and 16 audiences.");
    }
  });

  it("validates audience length in characters", () => {
    expect(credentialHeaderIssues([actorHeader(["😀".repeat(512)])])).toEqual([]);
    for (const invalid of ["", "a".repeat(513)]) {
      expect(credentialHeaderIssues([actorHeader([invalid])])).toContain(
        "Each actor token audience must contain between 1 and 512 characters.",
      );
    }
  });

  it("rejects duplicate and runtime audiences", () => {
    expect(credentialHeaderIssues([actorHeader(["service", "service"])]))
      .toContain("Actor token audiences must be unique.");
    expect(credentialHeaderIssues([actorHeader(["kagent-runtime"])]))
      .toContain("Actor token audiences cannot include kagent-runtime.");
  });

  it.each([
    {},
    { secretKeyRef: { name: "secret", key: "token" }, tokenPassthrough: {} },
    { actorToken: { audiences: ["service"] }, tokenPassthrough: {} },
    { actorToken: { audiences: ["service"] }, secretKeyRef: { name: "secret", key: "token" } },
  ])("requires exactly one credential source: %j", (source) => {
    const header = { name: "Authorization", valueFrom: source } as unknown as Header;
    expect(credentialHeaderIssues([header])).toContain("Each credential header needs exactly one credential source.");
  });

  it("retains validation for existing sources and header collisions", () => {
    const header = actorHeader();
    expect(credentialHeaderIssues([header], ["authorization"]))
      .toContain("Header Authorization is configured more than once.");
    expect(credentialHeaderIssues([
      { name: "Authorization", valueFrom: { secretKeyRef: { name: "", key: "" } } },
    ])).toContain("Each credential Secret needs a name and key.");
    expect(credentialHeaderIssues([{ name: "Authorization", valueFrom: { tokenPassthrough: {} } }])).toEqual([]);
  });
});
