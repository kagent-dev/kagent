import type { Agent } from "./agents";

export type AgentRevisionState = "ready" | "preparing" | "notReported";

/** Display projection of an explicit Agent resource. */
export interface AgentPair {
  id: string;
  name: string;
  namespace: string;
  agentTemplate: string;
  harness: string;
  description: string;
  revisionState: AgentRevisionState;
  latestSuccessfulRevision?: string;
  notReadyReason?: string;
  definition?: Agent;
  isUnmapped?: boolean;
}

export function agentPairsOf(agent: Agent): AgentPair[] {
  const { spec, status } = agent.resource;
  const failing = status?.conditions?.find((condition) => condition.status === "False");
  return [{
    id: agent.ref,
    name: agent.name,
    namespace: agent.namespace,
    agentTemplate: spec.templateRef?.name ?? "Inline",
    harness: spec.harnessRef?.name ?? "Inline",
    description: spec.template?.description ?? "",
    definition: agent,
    revisionState: status?.latestSuccessfulRevision
      ? "ready"
      : status?.desiredRevision ? "preparing" : "notReported",
    latestSuccessfulRevision: status?.latestSuccessfulRevision,
    notReadyReason: failing?.message ?? failing?.reason,
  }];
}

export function agentPairsFrom(agents: readonly Agent[]): AgentPair[] {
  return agents.flatMap(agentPairsOf).sort((a, b) => a.id.localeCompare(b.id));
}

export function pairIdOfInstance(instance: { agent?: string }): string | undefined {
  return instance.agent || undefined;
}

export function bareName(ref: string): string {
  return ref.slice(ref.lastIndexOf("/") + 1);
}

export function newConversationBlockedReason(pair: AgentPair): string | undefined {
  if (pair.revisionState === "ready") return undefined;
  if (pair.revisionState === "preparing") {
    return (
      pair.notReadyReason ??
      "The controller is still preparing a revision for this agent. Conversations can start once one has succeeded."
    );
  }
  if (pair.notReadyReason) return pair.notReadyReason;
  return "The controller has not reported a revision for this agent yet, so there is nothing to start a conversation from.";
}

export const UNMAPPED_AGENT_ID = "__unmapped__";
export const UNMAPPED_AGENT_NAME = "unmapped-agentinstances";

export function unmappedAgent(namespace: string): AgentPair {
  return {
    id: UNMAPPED_AGENT_ID,
    name: UNMAPPED_AGENT_NAME,
    namespace,
    agentTemplate: "",
    harness: "",
    description: "Conversations whose Agent no longer exists.",
    revisionState: "notReported",
    isUnmapped: true,
  };
}
