import type { AgentTemplate } from "./agentTemplates";
import type { Agent } from "./agents";

export type AgentRevisionState = "ready" | "preparing" | "notReported";

/** Display projection of an explicit Agent resource. */
export interface AgentSummary {
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

export function agentSummaryOf(agent: Agent, templates: readonly AgentTemplate[] = []): AgentSummary {
  const { spec, status } = agent.resource;
  const failing = status?.conditions?.find((condition) => condition.status === "False");
  const template = spec.template ?? templates.find((entry) => entry.namespace === agent.namespace && entry.name === spec.templateRef?.name)?.resource.spec;
  return {
    id: agent.ref,
    name: agent.name,
    namespace: agent.namespace,
    agentTemplate: spec.templateRef?.name ?? "Inline",
    harness: spec.harnessRef?.name ?? "Inline",
    description: template?.description ?? "",
    definition: agent,
    revisionState: status?.latestSuccessfulRevision
      ? "ready"
      : status?.desiredRevision ? "preparing" : "notReported",
    latestSuccessfulRevision: status?.latestSuccessfulRevision,
    notReadyReason: failing?.message ?? failing?.reason,
  };
}

export function agentSummariesFrom(agents: readonly Agent[], templates: readonly AgentTemplate[] = []): AgentSummary[] {
  return agents.map((agent) => agentSummaryOf(agent, templates)).sort((a, b) => a.id.localeCompare(b.id));
}

export function agentRefOfInstance(instance: { agent?: string }): string | undefined {
  return instance.agent || undefined;
}

export function bareName(ref: string): string {
  return ref.slice(ref.lastIndexOf("/") + 1);
}

export function newConversationBlockedReason(agent: AgentSummary): string | undefined {
  if (agent.revisionState === "ready") return undefined;
  if (agent.revisionState === "preparing") {
    return (
      agent.notReadyReason ??
      "The controller is still preparing a revision for this agent. Conversations can start once one has succeeded."
    );
  }
  if (agent.notReadyReason) return agent.notReadyReason;
  return "The controller has not reported a revision for this agent yet, so there is nothing to start a conversation from.";
}

export const UNMAPPED_AGENT_ID = "__unmapped__";
export const UNMAPPED_AGENT_NAME = "unmapped-agentinstances";

export function unmappedAgent(namespace: string): AgentSummary {
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
