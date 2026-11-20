import { referenceLabel, type McpToolBinding } from "@/api";

/** One line in a server's tool list. */
export interface ToolRow {
  /** `all` is a binding with no tool list; `allOther` is the same next to named tools. */
  kind: "tool" | "all" | "allOther";
  /** The tool's name; empty for `all` and `allOther`. */
  name: string;
  requiresApproval: boolean;
}

export type ServerApproval = "none" | "some" | "all";

/** Every binding for one MCP server, merged. */
export interface ToolServerGroup {
  /** Kind and label, so two servers of different kinds with one name stay apart. */
  key: string;
  server: string;
  rows: ToolRow[];
  /** How many named tools the agent lists for this server. */
  namedCount: number;
  /** Named tools only; the all-other-tools row has no count. */
  askCount: number;
  freeCount: number;
  /** Set next to named tools: whether every other tool on the server asks first. */
  otherAsks?: boolean;
  approval: ServerApproval;
}

/**
 * Merges MCP bindings into one group per server.
 *
 * A tool listed by several bindings asks first if any of them requires approval.
 * A binding with no tool list covers the whole server and becomes one row.
 */
export function groupMcpTools(
  bindings: readonly McpToolBinding[],
  namespace: string,
): ToolServerGroup[] {
  const byServer = new Map<string, { server: string; named: Map<string, boolean>; all?: boolean }>();

  for (const binding of bindings) {
    const server = referenceLabel(binding.server, namespace);
    const key = `${binding.server.kind ?? "RemoteMCPServer"}/${server}`;
    const entry = byServer.get(key) ?? { server, named: new Map<string, boolean>() };
    const requires = binding.requireApproval === true;
    if (binding.tools?.length) {
      for (const tool of binding.tools) {
        entry.named.set(tool, (entry.named.get(tool) ?? false) || requires);
      }
    } else {
      entry.all = (entry.all ?? false) || requires;
    }
    byServer.set(key, entry);
  }

  return [...byServer].map(([key, { server, named, all }]) => {
    const rows: ToolRow[] = [...named].map(([name, requiresApproval]) => ({
      kind: "tool",
      name,
      requiresApproval,
    }));
    if (all !== undefined) {
      rows.push({ kind: named.size > 0 ? "allOther" : "all", name: "", requiresApproval: all });
    }
    const tools = rows.filter((row) => row.kind === "tool");
    const askCount = tools.filter((row) => row.requiresApproval).length;
    const freeCount = tools.length - askCount;
    const asking = rows.filter((row) => row.requiresApproval).length;
    const approval: ServerApproval =
      asking === 0 ? "none" : asking === rows.length ? "all" : "some";
    const otherAsks = named.size > 0 && all !== undefined ? all : undefined;
    return { key, server, rows, namedCount: named.size, askCount, freeCount, otherAsks, approval };
  });
}
