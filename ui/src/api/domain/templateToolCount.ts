import { referenceLabel, type ToolBinding } from "./agentTemplates";
import type { ToolServerResponse } from "./mcpServers";

/** The tools a template exposes. `unknownServers` lists "all tools" servers with no discovered count. */
export interface TemplateToolCount {
  total: number;
  unknownServers: string[];
}

/**
 * Counts what a template exposes: named tools, every discovered tool of an "all tools"
 * server, and one per sub-agent.
 */
export function countTemplateTools(
  bindings: readonly ToolBinding[],
  servers: ReadonlyMap<string, ToolServerResponse>,
  ownerNamespace: string,
): TemplateToolCount {
  let total = 0;
  const unknownServers: string[] = [];
  for (const binding of bindings) {
    if (binding.subAgent) {
      total += 1;
    } else if (binding.mcp?.tools?.length) {
      total += new Set(binding.mcp.tools).size;
    } else if (binding.mcp) {
      const ref = referenceLabel(binding.mcp.server, ownerNamespace);
      const server = servers.get(ref);
      if (server) total += server.discoveredTools.length;
      else unknownServers.push(ref);
    }
  }
  return { total, unknownServers };
}
