import { describe, expect, it } from "vitest";
import type { ToolBinding } from "./agentTemplates";
import type { ToolServerResponse } from "./mcpServers";
import { countTemplateTools } from "./templateToolCount";

const server = (ref: string, names: string[]): ToolServerResponse => ({
  ref,
  groupKind: "RemoteMCPServer.api.kagent.dev",
  discoveredTools: names.map((name) => ({ name, description: "" })),
});
const servers = new Map([
  ["team/docs", server("team/docs", ["a", "b", "c"])],
  ["ops/k8s", server("ops/k8s", ["x", "y"])],
]);

describe("countTemplateTools", () => {
  it("counts named tools, an unrestricted server's discovered tools and sub-agents", () => {
    const bindings: ToolBinding[] = [
      { mcp: { server: { name: "docs" }, tools: ["a"] } },
      { mcp: { server: { name: "k8s", namespace: "ops" } } },
      { subAgent: { name: "helper", description: "", templateRef: { name: "helper" } } },
    ];
    expect(countTemplateTools(bindings, servers, "team")).toEqual({ total: 4, unknownServers: [] });
  });

  it("treats an empty tools list as every tool", () => {
    const bindings: ToolBinding[] = [{ mcp: { server: { name: "docs" }, tools: [] } }];
    expect(countTemplateTools(bindings, servers, "team").total).toBe(3);
  });

  it("names an unrestricted server it has no tools for instead of guessing", () => {
    const bindings: ToolBinding[] = [
      { mcp: { server: { name: "gone" } } },
      { mcp: { server: { name: "docs" }, tools: ["a", "b"] } },
    ];
    expect(countTemplateTools(bindings, servers, "team")).toEqual({ total: 2, unknownServers: ["team/gone"] });
  });
});
