/** The part of `SandboxTemplate` the UI reads. The rest of the CR is left alone. */
import type { ResourceMetadata } from "./common";
import type { AgentCondition } from "./agents";

export interface SandboxTemplateResource {
  metadata: ResourceMetadata;
  spec?: { egressPolicyRefs?: { name: string }[] };
  status?: { conditions?: AgentCondition[] };
}

export interface SandboxTemplate {
  ref: string;
  namespace: string;
  name: string;
  resource: SandboxTemplateResource;
}
