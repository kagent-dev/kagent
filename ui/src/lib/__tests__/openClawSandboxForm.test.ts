import { describe, expect, it } from "@jest/globals";
import {
  buildSandboxCRDraft,
  defaultOpenClawSandboxFormSlice,
  parseAllowedDomainsList,
  validateOpenClawSandboxForm,
} from "../openClawSandboxForm";

function withAllowedDomains(allowedDomains: string) {
  return { ...defaultOpenClawSandboxFormSlice(), allowedDomains };
}

describe("openClawSandboxForm allowedDomains", () => {
  describe("parseAllowedDomainsList", () => {
    it("returns an empty list for empty / whitespace input", () => {
      expect(parseAllowedDomainsList("")).toEqual([]);
      expect(parseAllowedDomainsList("   \n\t  ")).toEqual([]);
    });

    it("splits on newlines, commas, and whitespace", () => {
      expect(parseAllowedDomainsList("api.github.com\nregistry.npmjs.org")).toEqual([
        "api.github.com",
        "registry.npmjs.org",
      ]);
      expect(parseAllowedDomainsList("api.github.com, registry.npmjs.org   *.slack.com")).toEqual([
        "api.github.com",
        "registry.npmjs.org",
        "*.slack.com",
      ]);
    });

    it("dedupes case-insensitively and preserves first-seen order", () => {
      expect(parseAllowedDomainsList("API.github.com\napi.github.com\nRegistry.npmjs.org")).toEqual([
        "API.github.com",
        "Registry.npmjs.org",
      ]);
    });
  });

  describe("validateOpenClawSandboxForm", () => {
    it("accepts an empty allowedDomains list", () => {
      const result = validateOpenClawSandboxForm({
        openClaw: withAllowedDomains(""),
        modelRef: "ns/m1",
      });
      expect(result).toBeUndefined();
    });

    it("accepts plain hosts and glob labels", () => {
      const result = validateOpenClawSandboxForm({
        openClaw: withAllowedDomains("api.github.com\n*.slack.com\nregistry.npmjs.org"),
        modelRef: "ns/m1",
      });
      expect(result).toBeUndefined();
    });

    it.each([
      ["https://api.github.com", "scheme not allowed"],
      ["api.github.com/path", "path not allowed"],
      ["..", "empty labels"],
      ["-bad.example.com", "bad label start"],
    ])("rejects malformed entry %p (%s)", (entry) => {
      const result = validateOpenClawSandboxForm({
        openClaw: withAllowedDomains(entry),
        modelRef: "ns/m1",
      });
      expect(result).toMatch(/not a valid hostname/);
    });
  });

  describe("buildSandboxCRDraft", () => {
    it("omits spec.network when allowedDomains is empty", () => {
      const draft = buildSandboxCRDraft({
        name: "h1",
        namespace: "ns",
        description: "",
        modelRef: "m1",
        openClaw: withAllowedDomains(""),
      });
      expect("error" in draft).toBe(false);
      if ("error" in draft) return;
      expect(draft.spec.network).toBeUndefined();
    });

    it("writes spec.network.allowedDomains preserving order and deduping", () => {
      const draft = buildSandboxCRDraft({
        name: "h1",
        namespace: "ns",
        description: "",
        modelRef: "m1",
        openClaw: withAllowedDomains("api.github.com\nregistry.npmjs.org\napi.github.com\n*.slack.com"),
      });
      expect("error" in draft).toBe(false);
      if ("error" in draft) return;
      expect(draft.spec.network).toEqual({
        allowedDomains: ["api.github.com", "registry.npmjs.org", "*.slack.com"],
      });
    });

    it("targets the AgentHarness CR with the openclaw backend", () => {
      const draft = buildSandboxCRDraft({
        name: "h1",
        namespace: "ns",
        description: "",
        modelRef: "m1",
        openClaw: withAllowedDomains("api.github.com"),
      });
      expect("error" in draft).toBe(false);
      if ("error" in draft) return;
      expect(draft.apiVersion).toBe("kagent.dev/v1alpha2");
      expect(draft.kind).toBe("AgentHarness");
      expect(draft.spec.backend).toBe("openclaw");
    });
  });
});
