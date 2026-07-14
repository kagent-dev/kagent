import { describe, expect, it } from "@jest/globals";
import {
  buildAgentHarnessCRDraft,
  defaultAgentHarnessFormSlice,
  newAgentHarnessChannelRow,
  validateAgentHarnessForm,
} from "../agentHarnessForm";

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

  it("accepts a substrate harness without a gateway token (auto-generated)", () => {
    const r = validateAgentHarnessForm({
      harness: { ...defaultAgentHarnessFormSlice() },
      modelRef: "ns/m1",
    });
    expect(r).toBeUndefined();
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

describe("agentHarnessForm build", () => {
  describe("buildAgentHarnessCRDraft", () => {
    it("targets the AgentHarness CR with the openclaw backend", () => {
      const draft = buildAgentHarnessCRDraft({
        name: "h1",
        namespace: "ns",
        description: "",
        modelRef: "m1",
        harness: defaultAgentHarnessFormSlice(),
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
          substrateWorkerPoolRefName: "default-wp",
        },
      });
      expect("error" in draft).toBe(false);
      if ("error" in draft) return;
      expect(draft.spec.substrate).toEqual({
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
