import { describe, expect, it } from "vitest";
import type { ToolBinding } from "./agentTemplates";
import type { ToolServerResponse } from "./mcpServers";
import { countTemplateTools, indexToolServers } from "./templateToolCount";

const server = (ref: string, names: string[]): ToolServerResponse => ({
  ref,
  groupKind: "RemoteMCPServer.api.kagent.dev",
  discoveredTools: names.map((name) => ({ name, description: "" })),
});
const servers = indexToolServers([
  server("team/docs", ["a", "b", "c"]),
  server("ops/k8s", ["x", "y"]),
  { ...server("team/docs", ["s1", "s2", "s3", "s4", "s5"]), groupKind: "Service" },
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

  it("does not count a server twice when two bindings point at it", () => {
    const bindings: ToolBinding[] = [
      { mcp: { server: { name: "docs" } } },
      { mcp: { server: { name: "docs" }, tools: ["a"], requireApproval: true } },
    ];
    expect(countTemplateTools(bindings, servers, "team").total).toBe(3);
  });

  it("matches a server by kind as well as name", () => {
    const service: ToolBinding[] = [{ mcp: { server: { kind: "Service", name: "docs" } } }];
    expect(countTemplateTools(service, servers, "team").total).toBe(5);
    const other: ToolBinding[] = [{ mcp: { server: { group: "example.com", kind: "Foo", name: "docs" } } }];
    expect(countTemplateTools(other, servers, "team")).toEqual({ total: 0, unknownServers: ["team/docs"] });
  });
});
