import { paths } from "@/router/routes";

/** A conversation identified by UUID. */
export interface AgentInstanceRef { id: string; }

/**
 * Links to an instance's surfaces, from a ref that may not be complete yet.
 *
 * `buildPath` is the application's builder and throws on a missing value, which is
 * right for a caller that knows what it has. These are called from the rail, whose
 * route params are typed as possibly-undefined — so every link would need its own
 * guard first, and a missed one is a page that throws while rendering rather than a
 * link that goes nowhere. Falling back to the agents list is a link that goes
 * somewhere sensible.
 */
function fill(template: string, ref: Partial<AgentInstanceRef>): string {
  if (!ref.id) return paths.agents;
  return template.replace(":id", encodeURIComponent(ref.id));
}

/** Instance chat and details links. Editable configuration lives on the Agent. */
export const agentUrl = {
  details: (ref: Partial<AgentInstanceRef>) => fill(paths.agentDetail, ref),
  chat: (ref: Partial<AgentInstanceRef>) => fill(paths.agentChat, ref),
};

export interface AgentRef { namespace: string; name: string; }
export function agentPageUrl(ref: Partial<AgentRef>): string | undefined {
 if (!ref.namespace || !ref.name) return undefined;
 return paths.agent.replace(":namespace", encodeURIComponent(ref.namespace)).replace(":name", encodeURIComponent(ref.name));
}
export function agentNewChatUrl(ref: Partial<AgentRef>): string | undefined {
 const base = agentPageUrl(ref);
 return base ? `${base}/new` : undefined;
}
