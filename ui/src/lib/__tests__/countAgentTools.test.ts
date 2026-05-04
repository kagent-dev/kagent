import { describe, expect, it } from "@jest/globals";
import type { AgentResponse, Tool } from "@/types";
import { countAgentToolBindings, countToolBindings, getAgentToolsList } from "../countAgentTools";

const base: AgentResponse = {
  id: 1,
  agent: { metadata: { name: "a" }, spec: { type: "Declarative", description: "" } },
  model: "",
  modelProvider: "openai",
  modelConfigRef: "",
  tools: [],
  deploymentReady: true,
  accepted: true,
};

function toolMcp(names?: string[]): Tool {
  return {
    type: "McpServer",
    mcpServer: { name: "srv", namespace: "ns", toolNames: names, kind: "ToolServer" },
  };
}

describe("getAgentToolsList", () => {
  it("uses declarative spec when data.tools is empty", () => {
    const t = [toolMcp(["a", "b"])];
    const r: AgentResponse = {
      ...base,
      tools: [],
      agent: {
        ...base.agent,
        spec: { type: "Declarative", description: "", declarative: { systemMessage: "", modelConfig: "m", tools: t } },
      },
    };
    expect(getAgentToolsList(r)).toEqual(t);
  });
});

describe("countToolBindings", () => {
  it("sums toolNames for MCP, or 1 when unfiltered", () => {
    expect(countToolBindings([toolMcp(["x", "y", "z"])])).toBe(3);
    expect(countToolBindings([toolMcp([])])).toBe(1);
    expect(countToolBindings([toolMcp(undefined)])).toBe(1);
  });

  it("counts Agent ref as 1", () => {
    const t: Tool = { type: "Agent", agent: { name: "other", namespace: "n" } };
    expect(countToolBindings([t])).toBe(1);
  });
});

describe("countAgentToolBindings", () => {
  it("matches list from spec, not only top-level tools", () => {
    const t = [toolMcp(["a"])];
    const r: AgentResponse = {
      ...base,
      tools: [],
      agent: {
        ...base.agent,
        spec: { type: "Declarative", description: "", declarative: { systemMessage: "", modelConfig: "m", tools: t } },
      },
    };
    expect(countAgentToolBindings(r)).toBe(1);
  });
});
