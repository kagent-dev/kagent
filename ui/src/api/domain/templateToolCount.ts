import { referenceLabel, type ObjectReference, type ToolBinding } from "./agentTemplates";
import type { DiscoveredTool, ToolServerResponse } from "./mcpServers";

/** The tools a template exposes. `unknownServers` lists "all tools" servers with no discovered count. */
export interface TemplateToolCount {
  total: number;
  unknownServers: string[];
}

/** The `groupKind` the controller reports for a binding's server. Group and kind each default on their own. */
export function bindingGroupKind({ group, kind }: ObjectReference): string {
  const resolvedKind = kind || "RemoteMCPServer";
  const resolvedGroup = group ?? "api.kagent.dev";
  return resolvedGroup ? `${resolvedKind}.${resolvedGroup}` : resolvedKind;
}

const serverKey = (groupKind: string, ref: string) => `${groupKind}|${ref}`;

/** The server at `namespace/name` of `groupKind`, or of any kind when none is given. */
export function findToolServer(
  servers: readonly ToolServerResponse[],
  ref: string,
  groupKind?: string | null,
): ToolServerResponse | undefined {
  return servers.find((server) => server.ref === ref && (!groupKind || server.groupKind === groupKind));
}

/** Servers keyed by kind and `namespace/name`, since two kinds can share a name. */
export function indexToolServers(servers: readonly ToolServerResponse[]): Map<string, ToolServerResponse> {
  return new Map(servers.map((server) => [serverKey(server.groupKind, server.ref), server]));
}

/**
 * Counts what a template exposes: the distinct tools it takes from each server (every
 * discovered tool when a binding names none) plus one per agent template used as a tool.
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
        // A server kept in the catalog with discovery off or failed has no tools on record either.
        if (!server?.discoveredTools.length) {
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

/** The tools a template takes from one server, gathered across its bindings. */
export interface ServerToolGroup {
  server: ObjectReference;
  /** `namespace/name`. */
  ref: string;
  groupKind: string;
  /** True when some binding names no tools, which grants every tool. */
  allTools: boolean;
  /** The tools named, in binding order; empty when `allTools`. */
  selected: string[];
  /** What the server reported, or undefined when it is not in the catalog. */
  discovered?: DiscoveredTool[];
}

/** Groups a template's MCP bindings by server, in the order each server first appears. */
export function groupToolsByServer(
  bindings: readonly ToolBinding[],
  servers: ReadonlyMap<string, ToolServerResponse>,
  ownerNamespace: string,
): ServerToolGroup[] {
  const groups = new Map<string, ServerToolGroup>();
  for (const { mcp } of bindings) {
    if (!mcp) continue;
    const ref = referenceLabel(mcp.server, ownerNamespace);
    const groupKind = bindingGroupKind(mcp.server);
    const key = serverKey(groupKind, ref);
    const group = groups.get(key) ?? {
      server: mcp.server,
      ref,
      groupKind,
      allTools: false,
      selected: [],
      discovered: servers.get(key)?.discoveredTools,
    };
    if (!mcp.tools?.length) group.allTools = true;
    for (const name of mcp.tools ?? []) if (!group.selected.includes(name)) group.selected.push(name);
    groups.set(key, group);
  }
  return [...groups.values()].map((group) => (group.allTools ? { ...group, selected: [] } : group));
}
