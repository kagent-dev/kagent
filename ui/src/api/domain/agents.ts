import type { ResourceMetadata } from "./common";
import type { AgentTemplateSpec } from "./agentTemplates";
import type { HarnessSpec } from "./harnesses";

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
