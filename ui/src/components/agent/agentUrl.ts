import { paths } from "@/router/routes";

/** A conversation identified by UUID. */
export interface AgentRef { id: string; }

/**
 * Links to an agent's surfaces, from a ref that may not be complete yet.
 *
 * `buildPath` is the application's builder and throws on a missing value, which is
 * right for a caller that knows what it has. These are called from the rail, whose
 * route params are typed as possibly-undefined — so every link would need its own
 * guard first, and a missed one is a page that throws while rendering rather than a
 * link that goes nowhere. Falling back to the agents list is a link that goes
 * somewhere sensible.
 */
function fill(template: string, ref: Partial<AgentRef>): string {
  if (!ref.id) return paths.agents;
  return template.replace(":id", encodeURIComponent(ref.id));
}

/**
 * The two surfaces one agent has.
 *
 * There is no `edit`: an instance has no spec to change. What the agent *is* lives
 * on its `AgentTemplate` and how it *runs* on its `Harness`, so editing an agent
 * means editing one of those. And no `conversation`, because the instance is the
 * conversation — there is no session beneath it to link to.
 */
export const agentUrl = {
  details: (ref: Partial<AgentRef>) => fill(paths.agentDetail, ref),
  chat: (ref: Partial<AgentRef>) => fill(paths.agentChat, ref),
};

export interface AgentPairRef { namespace: string; name: string; }
export function agentPageUrl(ref: Partial<AgentPairRef>): string | undefined {
 if (!ref.namespace || !ref.name) return undefined;
 return paths.agent.replace(":namespace", encodeURIComponent(ref.namespace)).replace(":name", encodeURIComponent(ref.name));
}
export function agentNewChatUrl(ref: Partial<AgentPairRef>): string | undefined {
 const base = agentPageUrl(ref);
 return base ? `${base}/new` : undefined;
}
