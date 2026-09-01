import { describe, expect, it } from "vitest";
import { hasAgentResource, normaliseAgentResponse, usesSandboxA2APath } from "./agents";
import type { AgentResponse } from "./agents";

const row = {
  id: 1,
  agent: {
    metadata: { name: "k8s-agent", namespace: "kagent" },
    spec: { type: "Declarative", description: "" },
  },
  model: "gpt-4.1",
  modelProvider: "OpenAI",
  modelConfigRef: "kagent/default",
  deploymentReady: true,
  accepted: true,
  agentKind: "SandboxAgent",
} as unknown as Omit<AgentResponse, "tools" | "memoryRefs">;

describe("normaliseAgentResponse", () => {
  // Proto3 has no absent repeated field, so the live path always sends an array —
  // but an operation override or a fake can hand over neither, and the REST API
  // this replaced sent `null`. A `tools.length` in a table column took the whole
  // agents page down on it.
  it("reads an absent collection as an empty one", () => {
    expect(normaliseAgentResponse({ ...row, tools: null }).tools).toEqual([]);
    expect(normaliseAgentResponse({ ...row }).tools).toEqual([]);
    expect(normaliseAgentResponse({ ...row }).memoryRefs).toEqual([]);
  });

  it("leaves a populated collection alone", () => {
    const tools = [{ type: "McpServer" }] as AgentResponse["tools"];
    expect(normaliseAgentResponse({ ...row, tools }).tools).toBe(tools);
  });

  it("changes nothing else about the row", () => {
    const normalised: Record<string, unknown> = {
      ...normaliseAgentResponse({ ...row, tools: null }),
    };
    delete normalised.tools;
    delete normalised.memoryRefs;
    expect(normalised).toEqual(row);
  });
});

/**
 * Which kind an agent is decides what can be done to it, and the two answers
 * below are easy to get backwards — the previous API called an agent "sandbox"
 * when it had *no* resource behind it, and this one calls the ordinary, editable
 * agent a `SandboxAgent`. Pinned here so the inversion cannot be reintroduced
 * quietly.
 */
describe("what a kind allows", () => {
  const of = (kind: AgentResponse["agentKind"]) =>
    normaliseAgentResponse({ ...row, agentKind: kind });

  it("treats a sandbox agent as writable", () => {
    expect(hasAgentResource(of("SandboxAgent"))).toBe(true);
  });

  // `AgentService` has no UpdateAgentHarness, so a form that offered to save one
  // would have nothing to call.
  it("treats a harness as not writable", () => {
    expect(hasAgentResource(of("AgentHarness"))).toBe(false);
  });

  it("treats a kind it has never seen as not writable", () => {
    expect(hasAgentResource(of("unknown"))).toBe(false);
  });

  it("sends a sandbox agent's conversation to the sandbox A2A path", () => {
    expect(usesSandboxA2APath(of("SandboxAgent"))).toBe(true);
    expect(usesSandboxA2APath(of("AgentHarness"))).toBe(false);
  });
});
