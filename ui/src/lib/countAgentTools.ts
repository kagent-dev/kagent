import type { AgentResponse, Tool } from "@/types";

/**
 * Tool list for display/counts: `agent.spec` is the source of truth. The list
 * API can omit or clear `data.tools` when the ModelConfig lookup in the server
 * fails, even though the CRD in `data.agent` still has `declarative.tools`.
 */
export function getAgentToolsList(item: AgentResponse): Tool[] {
  return item.agent.spec?.declarative?.tools ?? item.tools ?? [];
}

/**
 * How many "tools" the agent exposes: for each MCP server binding, count
 * selected `toolNames` (or 1 if none listed = whole server). Each
 * `Agent` tool ref counts as 1.
 */
export function countToolBindings(tools: Tool[] | undefined): number {
  if (!tools?.length) {
    return 0;
  }
  let n = 0;
  for (const t of tools) {
    if (t.mcpServer) {
      const names = t.mcpServer.toolNames;
      n += names && names.length > 0 ? names.length : 1;
    } else if (t.agent) {
      n += 1;
    } else {
      n += 1;
    }
  }
  return n;
}

export function countAgentToolBindings(item: AgentResponse): number {
  return countToolBindings(getAgentToolsList(item));
}
