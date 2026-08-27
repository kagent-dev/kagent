import { paths } from "@/router/routes";

/**
 * How an agent is addressed: a namespace and an instance id.
 *
 * An agent *is* an `AgentInstance`, and an instance has no name — its id is a
 * UUID, which is what every one of its RPCs takes alongside the namespace. So this
 * carries an id where it used to carry a name, and the id is what appears in a URL.
 */
export interface AgentRef {
  namespace: string;
  id: string;
}

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
  if (!ref.namespace || !ref.id) return paths.agents;
  return template
    .replace(":namespace", encodeURIComponent(ref.namespace))
    .replace(":id", encodeURIComponent(ref.id));
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

/** How an agent is addressed: a namespace and the two halves of its pair. */
export interface AgentPairRef {
  namespace: string;
  agentTemplate: string;
  harness: string;
}

/**
 * The agent one conversation belongs to.
 *
 * `undefined` when the record names no pair, which is a real state rather than a
 * missing value: an instance with no prepared revision belongs to no pair, and the
 * controller's own list query joins it as `NULL`. A caller renders no link at all
 * in that case, rather than one that leads to an agent that does not exist.
 */
export function agentPageUrl(ref: Partial<AgentPairRef>): string | undefined {
  if (!ref.namespace || !ref.agentTemplate || !ref.harness) return undefined;
  return paths.agent
    .replace(":namespace", encodeURIComponent(ref.namespace))
    .replace(":agentTemplate", encodeURIComponent(ref.agentTemplate))
    .replace(":harness", encodeURIComponent(ref.harness));
}

/**
 * Where "start talking to this agent" goes.
 *
 * A conversation that does not exist yet, addressed by the agent. Nothing is created
 * until the first message is sent — see `AgentNewChatPage` for why that matters.
 */
export function agentNewChatUrl(ref: Partial<AgentPairRef>): string | undefined {
  if (!ref.namespace || !ref.agentTemplate || !ref.harness) return undefined;
  return paths.agentNewChat
    .replace(":namespace", encodeURIComponent(ref.namespace))
    .replace(":agentTemplate", encodeURIComponent(ref.agentTemplate))
    .replace(":harness", encodeURIComponent(ref.harness));
}
