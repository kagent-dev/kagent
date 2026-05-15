import { describe, expect, it } from "@jest/globals";
import {
  buildSandboxCRDraft,
  defaultOpenClawSandboxFormSlice,
  newOpenClawChannelRow,
  parseAllowedDomainsList,
  validateOpenClawSandboxForm,
} from "../openClawSandboxForm";

function withAllowedDomains(allowedDomains: string) {
  return { ...defaultOpenClawSandboxFormSlice(), allowedDomains };
}

describe("validateOpenClawSandboxForm sections", () => {
  it("tags missing model as general", () => {
    expect(
      validateOpenClawSandboxForm({
        openClaw: defaultOpenClawSandboxFormSlice(),
        modelRef: "",
      }),
    ).toEqual({
      section: "general",
      message: "Please select a model config for this sandbox.",
    });
  });

  it("tags allowed domain failures as allowedDomains", () => {
    const r = validateOpenClawSandboxForm({
      openClaw: withAllowedDomains("https://api.github.com"),
      modelRef: "ns/m1",
    });
    expect(r?.section).toBe("allowedDomains");
    expect(r?.message).toContain("not a valid hostname");
  });

  it("tags channel credential failures as channels", () => {
    const row = newOpenClawChannelRow();
    row.name = "slack1";
    row.channelType = "slack";
    row.botToken = "";
    const r = validateOpenClawSandboxForm({
      openClaw: { ...defaultOpenClawSandboxFormSlice(), channels: [row] },
      modelRef: "ns/m1",
    });
    expect(r?.section).toBe("channels");
    expect(r?.message).toContain("slack1");
  });
});

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
      expect(result?.section).toBe("allowedDomains");
      expect(result?.message).toMatch(/not a valid hostname/);
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
