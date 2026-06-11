import { describe, expect, it } from "@jest/globals";
import {
  buildAgentHarnessCRDraft,
  defaultAgentHarnessFormSlice,
  newAgentHarnessChannelRow,
  parseAllowedDomainsList,
  validateAgentHarnessForm,
} from "../agentHarnessForm";

function withAllowedDomains(allowedDomains: string) {
  return { ...defaultAgentHarnessFormSlice(), allowedDomains };
}

describe("validateAgentHarnessForm sections", () => {
  it("tags missing model as general", () => {
    expect(
      validateAgentHarnessForm({
        harness: defaultAgentHarnessFormSlice(),
        modelRef: "",
      }),
    ).toEqual({
      section: "general",
      message: "Please select a model config for this AgentHarness.",
    });
  });

  it("tags allowed domain failures as allowedDomains", () => {
    const r = validateAgentHarnessForm({
      harness: withAllowedDomains("https://api.github.com"),
      modelRef: "ns/m1",
    });
    expect(r?.section).toBe("allowedDomains");
    expect(r?.message).toContain("not a valid hostname");
  });

  it("tags missing substrate gateway token as general", () => {
    const r = validateAgentHarnessForm({
      harness: { ...defaultAgentHarnessFormSlice(), runtime: "substrate" },
      modelRef: "ns/m1",
    });
    expect(r?.section).toBe("general");
    expect(r?.message).toContain("gateway token");
  });

  it("tags channel credential failures as channels", () => {
    const row = newAgentHarnessChannelRow();
    row.name = "slack1";
    row.channelType = "slack";
    row.botToken = "";
    const r = validateAgentHarnessForm({
      harness: { ...defaultAgentHarnessFormSlice(), channels: [row] },
      modelRef: "ns/m1",
    });
    expect(r?.section).toBe("channels");
    expect(r?.message).toContain("slack1");
  });

  it("rejects duplicate channel binding names", () => {
    const row = newAgentHarnessChannelRow();
    row.name = "dup";
    row.channelType = "telegram";
    row.botToken = "token-a";
    const row2 = newAgentHarnessChannelRow();
    row2.name = "dup";
    row2.channelType = "telegram";
    row2.botToken = "token-b";
    const r = validateAgentHarnessForm({
      harness: { ...defaultAgentHarnessFormSlice(), channels: [row, row2] },
      modelRef: "ns/m1",
    });
    expect(r?.section).toBe("channels");
    expect(r?.message).toContain("Duplicate");
  });

  it("requires Slack allowlist channels when backend is unset (defaults to openclaw)", () => {
    const row = newAgentHarnessChannelRow();
    row.name = "slack1";
    row.channelType = "slack";
    row.botToken = "xoxb-test";
    row.appToken = "xapp-test";
    row.channelAccess = "allowlist";
    row.allowlistChannels = "";
    const r = validateAgentHarnessForm({
      harness: { ...defaultAgentHarnessFormSlice(), channels: [row] },
      modelRef: "ns/m1",
    });
    expect(r?.section).toBe("channels");
    expect(r?.message).toContain("allowlist");
  });
});

describe("agentHarnessForm allowedDomains", () => {
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

  describe("validateAgentHarnessForm", () => {
    it("accepts an empty allowedDomains list", () => {
      const result = validateAgentHarnessForm({
        harness: withAllowedDomains(""),
        modelRef: "ns/m1",
      });
      expect(result).toBeUndefined();
    });

    it("accepts plain hosts and glob labels", () => {
      const result = validateAgentHarnessForm({
        harness: withAllowedDomains("api.github.com\n*.slack.com\nregistry.npmjs.org"),
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
      const result = validateAgentHarnessForm({
        harness: withAllowedDomains(entry),
        modelRef: "ns/m1",
      });
      expect(result?.section).toBe("allowedDomains");
      expect(result?.message).toMatch(/not a valid hostname/);
    });
  });

  describe("buildAgentHarnessCRDraft", () => {
    it("omits spec.network when allowedDomains is empty", () => {
      const draft = buildAgentHarnessCRDraft({
        name: "h1",
        namespace: "ns",
        description: "",
        modelRef: "m1",
        harness: withAllowedDomains(""),
      });
      expect("error" in draft).toBe(false);
      if ("error" in draft) return;
      expect(draft.spec.network).toBeUndefined();
    });

    it("writes spec.network.allowedDomains preserving order and deduping", () => {
      const draft = buildAgentHarnessCRDraft({
        name: "h1",
        namespace: "ns",
        description: "",
        modelRef: "m1",
        harness: withAllowedDomains("api.github.com\nregistry.npmjs.org\napi.github.com\n*.slack.com"),
      });
      expect("error" in draft).toBe(false);
      if ("error" in draft) return;
      expect(draft.spec.network).toEqual({
        allowedDomains: ["api.github.com", "registry.npmjs.org", "*.slack.com"],
      });
    });

    it("targets the AgentHarness CR with the openclaw backend", () => {
      const draft = buildAgentHarnessCRDraft({
        name: "h1",
        namespace: "ns",
        description: "",
        modelRef: "m1",
        harness: withAllowedDomains("api.github.com"),
      });
      expect("error" in draft).toBe(false);
      if ("error" in draft) return;
      expect(draft.apiVersion).toBe("kagent.dev/v1alpha2");
      expect(draft.kind).toBe("AgentHarness");
      expect(draft.spec.backend).toBe("openclaw");
    });

    it("writes substrate config without creating a WorkerPool", () => {
      const draft = buildAgentHarnessCRDraft({
        name: "h1",
        namespace: "ns",
        description: "",
        modelRef: "m1",
        harness: {
          ...defaultAgentHarnessFormSlice(),
          runtime: "substrate",
          substrateGatewayToken: "tok",
          substrateWorkerPoolRefName: "default-wp",
        },
      });
      expect("error" in draft).toBe(false);
      if ("error" in draft) return;
      expect(draft.spec.substrate).toEqual({
        gatewayToken: "tok",
        snapshotsConfig: { location: "gs://ate-snapshots/kagent/" },
        workerPoolRef: { name: "default-wp" },
      });
      expect(draft.spec.substrate).not.toHaveProperty("workerPool");
    });

    it("writes Hermes slack allowedUserIDs and home channel fields", () => {
      const row = newAgentHarnessChannelRow();
      row.name = "slack-main";
      row.channelType = "slack";
      row.botToken = "xoxb-test";
      row.appToken = "xapp-test";
      row.allowedSlackUserIDs = "U01234567 U89ABCDEF";
      row.slackHomeChannel = "C01234567890";
      row.slackHomeChannelName = "general";
      const draft = buildAgentHarnessCRDraft({
        name: "h1",
        namespace: "ns",
        description: "",
        modelRef: "m1",
        harness: { ...defaultAgentHarnessFormSlice(), backend: "hermes", channels: [row] },
      });
      expect("error" in draft).toBe(false);
      if ("error" in draft) return;
      const channels = draft.spec.channels as { slack: { hermes: Record<string, unknown> } }[];
      expect(channels[0].slack.hermes.allowedUserIDs).toEqual(["U01234567", "U89ABCDEF"]);
      expect(channels[0].slack.hermes.homeChannel).toBe("C01234567890");
      expect(channels[0].slack.hermes.homeChannelName).toBe("general");
      expect(channels[0].slack).not.toHaveProperty("openclaw");
    });
  });
});
