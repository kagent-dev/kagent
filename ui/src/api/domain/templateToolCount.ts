import { referenceLabel, type ObjectReference, type ToolBinding } from "./agentTemplates";
import type { ToolServerResponse } from "./mcpServers";

/** The tools a template exposes. `unknownServers` lists "all tools" servers with no discovered count. */
export interface TemplateToolCount {
  total: number;
  unknownServers: string[];
}

const REMOTE_MCP_GROUP_KIND = "RemoteMCPServer.api.kagent.dev";

/** The `groupKind` the controller reports for a binding's server; an omitted kind is a RemoteMCPServer. */
function bindingGroupKind({ group, kind }: ObjectReference): string {
  if (!kind || kind === "RemoteMCPServer") return REMOTE_MCP_GROUP_KIND;
  return group ? `${kind}.${group}` : kind;
}

const serverKey = (groupKind: string, ref: string) => `${groupKind}|${ref}`;

/** Servers keyed by kind and `namespace/name`, since two kinds can share a name. */
export function indexToolServers(servers: readonly ToolServerResponse[]): Map<string, ToolServerResponse> {
  return new Map(servers.map((server) => [serverKey(server.groupKind, server.ref), server]));
}

/**
 * Counts what a template exposes: the distinct tools it takes from each server (every
 * discovered tool when a binding names none) plus one per sub-agent.
 */
export function countTemplateTools(
  bindings: readonly ToolBinding[],
  servers: ReadonlyMap<string, ToolServerResponse>,
  ownerNamespace: string,
): TemplateToolCount {
  let subAgents = 0;
  const named = new Map<string, Set<string>>();
  const unknownServers = new Set<string>();
  for (const binding of bindings) {
    if (binding.subAgent) {
      subAgents += 1;
    } else if (binding.mcp) {
      const ref = referenceLabel(binding.mcp.server, ownerNamespace);
      const key = serverKey(bindingGroupKind(binding.mcp.server), ref);
      let names = binding.mcp.tools;
      if (!names?.length) {
        const server = servers.get(key);
        if (!server) {
          unknownServers.add(ref);
          continue;
        }
        names = server.discoveredTools.map((tool) => tool.name);
      }
      const seen = named.get(key) ?? new Set<string>();
      names.forEach((name) => seen.add(name));
      named.set(key, seen);
    }
  }
  let total = subAgents;
  for (const names of named.values()) total += names.size;
  return { total, unknownServers: [...unknownServers] };
}
