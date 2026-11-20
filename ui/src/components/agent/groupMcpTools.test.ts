import { describe, expect, it } from "vitest";
import { groupMcpTools } from "./groupMcpTools";
import type { McpToolBinding } from "@/api";

const server = { kind: "RemoteMCPServer", name: "tools" };
const binding = (tools?: string[], requireApproval?: boolean): McpToolBinding => ({
  server,
  tools,
  requireApproval,
});

describe("groupMcpTools", () => {
  it("lists one binding's tools with no approval", () => {
    const [group] = groupMcpTools([binding(["a", "b"])], "ns");
    expect(group.server).toBe("ns/tools");
    expect(group.rows.map((row) => row.name)).toEqual(["a", "b"]);
    expect(group.approval).toBe("none");
  });

  it("merges two bindings on one server and keeps each tool's own approval", () => {
    const groups = groupMcpTools([binding(["a", "b"]), binding(["c"], true)], "ns");
    expect(groups).toHaveLength(1);
    expect(groups[0].rows.map((row) => [row.name, row.requiresApproval])).toEqual([
      ["a", false],
      ["b", false],
      ["c", true],
    ]);
    expect(groups[0]).toMatchObject({ askCount: 1, freeCount: 2, approval: "some" });
  });

  it("shows an all-tools binding as one row, for an empty or omitted list", () => {
    for (const tools of [undefined, []]) {
      const [group] = groupMcpTools([binding(tools, true)], "ns");
      expect(group.rows).toEqual([{ kind: "all", name: "", requiresApproval: true }]);
      expect(group.approval).toBe("all");
    }
  });

  it("adds an 'all other tools' row beside named tools", () => {
    const [group] = groupMcpTools([binding(["a"]), binding(undefined, true)], "ns");
    expect(group.rows).toEqual([
      { kind: "tool", name: "a", requiresApproval: false },
      { kind: "allOther", name: "", requiresApproval: true },
    ]);
    expect(group.namedCount).toBe(1);
  });

  it("asks first for a duplicate tool if any binding requires approval", () => {
    const [group] = groupMcpTools([binding(["a"], true), binding(["a"], false)], "ns");
    expect(group.rows).toEqual([{ kind: "tool", name: "a", requiresApproval: true }]);
  });

  it("keeps servers apart", () => {
    const other: McpToolBinding = { server: { kind: "RemoteMCPServer", name: "x" }, tools: ["z"] };
    expect(groupMcpTools([binding(["a"]), other], "ns").map((g) => g.server)).toEqual([
      "ns/tools",
      "ns/x",
    ]);
  });
});
