import type { ResourceMetadata } from "./common";
import type { AgentTemplateSpec } from "./agentTemplates";
import type { HarnessSpec } from "./harnesses";

/** Exactly one of each pair; refs resolve in the Agent's namespace. */
export type AgentSpec =
  ({ template: AgentTemplateSpec; templateRef?: never } | { templateRef: { name: string }; template?: never }) &
  ({ harness: HarnessSpec; harnessRef?: never } | { harnessRef: { name: string }; harness?: never });
/** One condition the controller recorded for an Agent. */
export interface AgentCondition {
  type: string;
  status: string;
  reason?: string;
  message?: string;
}

export interface AgentStatus {
  observedGeneration?: number;
  desiredRevision?: string;
  latestSuccessfulRevision?: string;
  warnings?: string[];
  conditions?: AgentCondition[];
}

export interface AgentResource {
  metadata: ResourceMetadata;
  spec: AgentSpec;
  status?: AgentStatus;
}

export interface Agent {
  ref: string;
  namespace: string;
  name: string;
  resource: AgentResource;
}

/**
 * Whether the agent can run (`latestSuccessfulRevision`) and whether its current spec is healthy.
 * The controller keeps the last good revision when a later edit fails, so those differ.
 */
export type AgentRevisionState =
  | "ready"
  | "updating"
  | "updateFailed"
  | "preparing"
  | "failed"
  | "notReported";

export function agentRevisionState({ resource: { metadata, status } }: Agent): AgentRevisionState {
  // Ready=False only while the golden snapshot is pending; any other False condition is a failure.
  const failed = status?.conditions?.some((condition) => condition.status === "False"
    && !(condition.type === "Ready" && condition.reason === "ActorTemplatePending"));
  const behind = (metadata.generation !== undefined && (status?.observedGeneration ?? 0) < metadata.generation)
    || (Boolean(status?.desiredRevision) && status?.desiredRevision !== status?.latestSuccessfulRevision);
  if (status?.latestSuccessfulRevision) {
    if (failed) return "updateFailed";
    return behind ? "updating" : "ready";
  }
  if (failed) return "failed";
  return status?.desiredRevision || behind ? "preparing" : "notReported";
}

/** A conversation starts from the last successful revision, even while a newer one is failing. */
export function isAgentRunnable(agent: Agent): boolean {
  return Boolean(agent.resource.status?.latestSuccessfulRevision);
}

export function agentNotReadyReason({ resource: { status } }: Agent): string | undefined {
  const failing = status?.conditions?.find((condition) => condition.status === "False");
  return failing?.message ?? failing?.reason;
}

export function newConversationBlockedReason(agent: Agent): string | undefined {
  if (isAgentRunnable(agent)) return undefined;
  const state = agentRevisionState(agent);
  return agentNotReadyReason(agent) ?? (state === "preparing"
    ? "The controller is still preparing a revision for this agent. Conversations can start once one has succeeded."
    : "The controller has not reported a revision for this agent yet, so there is nothing to start a conversation from.");
}

/** `namespace/name` → `name`. */
export function bareName(ref: string): string {
  return ref.slice(ref.lastIndexOf("/") + 1);
}

/** The shared template's name, or undefined when the template is inline. */
export function templateRefName(agent: Agent): string | undefined {
  return agent.resource.spec.templateRef?.name;
}

/** The shared harness's name, or undefined when the harness is inline. */
export function harnessRefName(agent: Agent): string | undefined {
  return agent.resource.spec.harnessRef?.name;
}
